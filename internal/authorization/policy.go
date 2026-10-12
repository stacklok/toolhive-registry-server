// Package authorization owns standalone instance admission, outside raw catalog contracts.
package authorization

import (
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/types"
	expast "github.com/cedar-policy/cedar-go/x/exp/ast"
	"github.com/cedar-policy/cedar-go/x/exp/schema"
	"github.com/cedar-policy/cedar-go/x/exp/schema/validate"
)

// Action is a closed standalone security operation.
type Action string

// Closed standalone actions; adding one requires schema, route and audit coverage.
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

const maxPolicyBytes = 1 << 20

//go:embed schema.cedarschema
var schemaBytes []byte

var (
	// ErrDenied means no permit or a matching forbid.
	ErrDenied = errors.New("operation not permitted")
	// ErrEvaluation means the policy evaluator cannot produce a clean decision.
	ErrEvaluation = errors.New("authorization evaluation unavailable")
	// ErrEvidence is a fail-closed internal wiring fault.
	ErrEvidence = errors.New("authorization evidence missing or inconsistent")
	instance    = types.EntityUID{Type: "Registry::Instance", ID: "catalog"}
)

// Engine contains immutable policies loaded once, never a file watcher.
type Engine struct {
	policies *cedar.PolicySet
	revision string
}

// Revision identifies the exact bytes loaded into this immutable engine.
// It never rereads the policy file; use it when binding startup review evidence.
func (e *Engine) Revision() string {
	return "sha256:" + e.revision
}

// Load rejects missing, empty, oversized, unparsable and ill-typed policy files.
// Parser/validator errors deliberately do not escape: they may contain policy text.
func Load(path string) (*Engine, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("policy path must be absolute")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, errors.New("policy file unavailable")
	}
	st, err := os.Stat(resolved)
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxPolicyBytes {
		return nil, errors.New("policy must be a bounded regular file")
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, errors.New("policy file unavailable")
	}
	defer f.Close()
	st, err = f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxPolicyBytes {
		return nil, errors.New("policy must be a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxPolicyBytes+1))
	if err != nil || len(data) > maxPolicyBytes {
		return nil, errors.New("policy file unreadable or oversized")
	}
	return parse(data)
}

func parse(data []byte) (*Engine, error) {
	policies, err := cedar.NewPolicySetFromBytes("policy", data)
	if err != nil {
		return nil, errors.New("policy syntax invalid")
	}
	var s schema.Schema
	if err := s.UnmarshalCedar(schemaBytes); err != nil {
		return nil, errors.New("application schema invalid")
	}
	resolved, err := s.Resolve()
	if err != nil {
		return nil, errors.New("application schema unresolved")
	}
	validator := validate.New(resolved, validate.WithStrict())
	count := 0
	for id, policy := range policies.All() {
		count++
		if err := validator.Policy(string(id), (*expast.Policy)(policy.AST())); err != nil {
			return nil, errors.New("policy schema validation failed")
		}
	}
	if count == 0 {
		return nil, errors.New("policy must contain at least one statement")
	}
	return &Engine{policies: policies, revision: fmt.Sprintf("%x", sha256.Sum256(data))}, nil
}

func known(action Action) bool {
	switch action {
	case CatalogRead, MetadataRead, SourcesRead, SourcesWrite, SourcesDelete,
		ViewsRead, ViewsWrite, ViewsDelete, EntriesPublish, EntriesDelete:
		return true
	}
	return false
}

func (e *Engine) evaluate(principal types.Entity, action Action) error {
	if e == nil || e.policies == nil {
		return ErrEvaluation
	}
	if !known(action) {
		return ErrEvidence
	}
	entities := types.EntityMap{principal.UID: principal, instance: {UID: instance}}
	decision, diagnostic := cedar.Authorize(e.policies, entities, cedar.Request{
		Principal: principal.UID, Action: types.EntityUID{Type: "Registry::Action", ID: types.String(action)}, Resource: instance,
	})
	if len(diagnostic.Errors) != 0 {
		return ErrEvaluation
	}
	if decision != cedar.Allow {
		return ErrDenied
	}
	return nil
}
