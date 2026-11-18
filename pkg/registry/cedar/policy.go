package cedar

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	cedargo "github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/types"
	expast "github.com/cedar-policy/cedar-go/x/exp/ast"
	"github.com/cedar-policy/cedar-go/x/exp/schema"
	"github.com/cedar-policy/cedar-go/x/exp/schema/validate"
)

// DefaultPolicy is the policy [Load] uses when no path is given. It permits
// every action on every resource to any caller that reaches it. A custom policy
// replaces it entirely; the two are never combined.
const DefaultPolicy = "permit(principal, action, resource);\n"

// MaxPolicyBytes is the largest policy [Load] and [Parse] accept.
const MaxPolicyBytes = 1 << 20

// Action is one of the closed set of Cedar actions in the registry schema.
type Action string

// Actions in the registry schema. CatalogRead applies to a Registry::View;
// all others apply to Registry::Instance::"catalog".
const (
	CatalogRead    Action = "catalog.read"
	MetadataRead   Action = "metadata.read"
	SourcesRead    Action = "sources.read"
	SourcesWrite   Action = "sources.write"
	SourcesDelete  Action = "sources.delete"
	ViewsRead      Action = "views.read"
	ViewsWrite     Action = "views.write"
	ViewsDelete    Action = "views.delete"
	EntriesPublish Action = "entries.publish"
	EntriesDelete  Action = "entries.delete"
)

var (
	// ErrEvaluation means Cedar could not produce a clean decision. Any
	// diagnostic error counts, even when the raw decision was allow.
	ErrEvaluation = errors.New("cedar: authorization evaluation failed")
	// ErrInvalidIdentity means the verified identity could not be turned into
	// a principal, for example because a mapped claim is malformed.
	ErrInvalidIdentity = errors.New("cedar: invalid identity")
	// ErrAuthenticationRequired means an anonymous caller asked for an action
	// that only authenticated callers may reach.
	ErrAuthenticationRequired = errors.New("cedar: authentication required")
)

const (
	actionType    = types.EntityType("Registry::Action")
	principalType = types.EntityType("Registry::Principal")
	viewType      = types.EntityType("Registry::View")
	instanceType  = types.EntityType("Registry::Instance")
)

//go:embed schema.cedarschema
var schemaBytes []byte

// instance is the single resource for every action except catalog.read.
var instance = types.EntityUID{Type: instanceType, ID: "catalog"}

// schemaValidator parses the embedded schema once. It is constant, so a failure
// is a build defect, but it is still reported as an error rather than a panic.
var schemaValidator = sync.OnceValues(func() (*validate.Validator, error) {
	var s schema.Schema
	if err := s.UnmarshalCedar(schemaBytes); err != nil {
		return nil, errors.New("cedar: embedded schema invalid")
	}
	resolved, err := s.Resolve()
	if err != nil {
		return nil, errors.New("cedar: embedded schema unresolved")
	}
	return validate.New(resolved, validate.WithStrict()), nil
})

// Engine is an immutable, validated Cedar policy set. It is safe for
// concurrent use.
type Engine struct {
	policies *cedargo.PolicySet
	revision string
}

// Load returns an Engine for the policy file at path. An empty path uses
// [DefaultPolicy].
//
// A non-empty path must be absolute. Symlinks are resolved, and the target must
// be a regular file of at most [MaxPolicyBytes]; this is checked before and
// after opening, and the read itself is bounded. The policy must parse,
// validate strictly against the embedded schema and contain at least one
// statement. Errors are deliberately generic and never include policy text.
func Load(path string) (*Engine, error) {
	if path == "" {
		return Parse([]byte(DefaultPolicy))
	}
	data, err := readPolicyFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse returns an Engine for policy text supplied by an embedder. It applies
// the same size, syntax, strict schema validation and non-empty checks as
// [Load], with the same generic errors.
func Parse(policy []byte) (*Engine, error) {
	if len(policy) > MaxPolicyBytes {
		return nil, errors.New("cedar: policy exceeds size limit")
	}
	policies, err := cedargo.NewPolicySetFromBytes("policy", policy)
	if err != nil {
		return nil, errors.New("cedar: policy syntax invalid")
	}
	validator, err := schemaValidator()
	if err != nil {
		return nil, err
	}
	count := 0
	for id, p := range policies.All() {
		count++
		if err := validator.Policy(string(id), (*expast.Policy)(p.AST())); err != nil {
			return nil, errors.New("cedar: policy schema validation failed")
		}
	}
	if count == 0 {
		return nil, errors.New("cedar: policy must contain at least one statement")
	}
	sum := sha256.Sum256(policy)
	return &Engine{policies: policies, revision: "sha256:" + hex.EncodeToString(sum[:])}, nil
}

// Revision identifies the exact policy bytes loaded, as "sha256:<hex>". It
// never rereads the policy file.
func (e *Engine) Revision() string {
	return e.revision
}

// Valid reports whether a is one of the schema's actions.
func (a Action) Valid() bool {
	switch a {
	case CatalogRead, MetadataRead, SourcesRead, SourcesWrite, SourcesDelete,
		ViewsRead, ViewsWrite, ViewsDelete, EntriesPublish, EntriesDelete:
		return true
	}
	return false
}

func readPolicyFile(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("cedar: policy path must be absolute")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, errors.New("cedar: policy file unavailable")
	}
	st, err := os.Stat(resolved)
	if err != nil || !boundedRegular(st) {
		return nil, errors.New("cedar: policy must be a regular file within the size limit")
	}
	f, err := os.Open(resolved) //nolint:gosec // The path is operator configuration, validated above.
	if err != nil {
		return nil, errors.New("cedar: policy file unavailable")
	}
	defer func() { _ = f.Close() }()
	st, err = f.Stat()
	if err != nil || !boundedRegular(st) {
		return nil, errors.New("cedar: policy must be a regular file within the size limit")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxPolicyBytes+1))
	if err != nil || len(data) > MaxPolicyBytes {
		return nil, errors.New("cedar: policy file unreadable or over the size limit")
	}
	return data, nil
}

func boundedRegular(st os.FileInfo) bool {
	return st.Mode().IsRegular() && st.Size() <= MaxPolicyBytes
}

// evaluate returns whether principal may perform action on resource. A
// non-nil error means no clean decision was reached and the request must fail
// closed, even if Cedar's raw decision was allow.
func (e *Engine) evaluate(principal types.Entity, action Action, resource types.EntityUID) (bool, error) {
	if e == nil || e.policies == nil || !action.Valid() {
		return false, ErrEvaluation
	}
	if action == CatalogRead && (resource.Type != viewType || resource.ID == "") ||
		action != CatalogRead && resource != instance {
		return false, ErrEvaluation
	}
	entities := types.EntityMap{principal.UID: principal, resource: {UID: resource}}
	decision, diagnostic := cedargo.Authorize(e.policies, entities, cedargo.Request{
		Principal: principal.UID,
		Action:    types.EntityUID{Type: actionType, ID: types.String(action)},
		Resource:  resource,
	})
	if len(diagnostic.Errors) != 0 {
		return false, ErrEvaluation
	}
	return decision == cedargo.Allow, nil
}
