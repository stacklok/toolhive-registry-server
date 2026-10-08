// Package persistence defines the storage-neutral contract for source and named-view definitions.
// It does not authorize callers; hosts must enforce their own policy before invoking it.
package persistence

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"time"
)

// Errors returned by definition backends can be classified with errors.Is.
var (
	// ErrInvalid means the submitted definition does not satisfy the contract.
	ErrInvalid = errors.New("invalid definition")
	// ErrNotFound means the requested definition or reference does not exist.
	ErrNotFound = errors.New("definition not found")
	// ErrConflict means an immutable property or ownership constraint was violated.
	ErrConflict = errors.New("definition conflict")
	// ErrInUse means a referenced source cannot be removed.
	ErrInUse = errors.New("source in use")
	// ErrUnavailable means storage could not be reached or returned an unclassified backend error.
	ErrUnavailable = errors.New("definition storage unavailable")
)

// Origin records which writer owns a definition.
type Origin string

// The two immutable origins of definitions.
const (
	OriginAPI    Origin = "API"
	OriginConfig Origin = "CONFIG"
)

// GitSpec uses the existing source_config JSON object (not a new catalog schema).
// Exactly one spec must be non-nil. Credentials are referenced by file path, never stored as plaintext.
type GitSpec struct {
	Repository string   `json:"repository"`
	Branch     string   `json:"branch,omitempty"`
	Tag        string   `json:"tag,omitempty"`
	Commit     string   `json:"commit,omitempty"`
	Path       string   `json:"path,omitempty"`
	Auth       *GitAuth `json:"auth,omitempty"`
}

// GitAuth references a credential file, not a credential value.
type GitAuth struct {
	Username     string `json:"username,omitempty"`
	PasswordFile string `json:"passwordFile,omitempty"`
}

// APISpec configures an upstream registry endpoint.
type APISpec struct {
	Endpoint string `json:"endpoint"`
	Timeout  string `json:"timeout,omitempty"`
}

// FileSpec selects one local path, remote URL, or inline payload.
type FileSpec struct {
	Path    string `json:"path,omitempty"`
	URL     string `json:"url,omitempty"`
	Data    string `json:"data,omitempty"`
	Timeout string `json:"timeout,omitempty"`
}

// ManagedSpec identifies the singleton API-published source.
type ManagedSpec struct{}

// KubernetesSpec selects namespaces for discovery.
type KubernetesSpec struct {
	Namespaces []string `json:"namespaces,omitempty"`
}

// NameFilter includes or excludes entry names or tags.
type NameFilter struct {
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

// Filter retains the existing name/tag filter shape.
type Filter struct {
	Names *NameFilter `json:"names,omitempty"`
	Tags  *NameFilter `json:"tags,omitempty"`
}

// SourceDefinition is a claims-free definition; ID and Origin are assigned at creation.
// Schedule is required for git/api/file path or URL and absent for managed,
// kubernetes and inline file data.
// Filter uses the existing filter_config column; inline file data may be sensitive.
type SourceDefinition struct {
	ID         string          `json:"id,omitempty"`
	Name       string          `json:"name"`
	Origin     Origin          `json:"origin,omitempty"`
	Git        *GitSpec        `json:"git,omitempty"`
	API        *APISpec        `json:"api,omitempty"`
	File       *FileSpec       `json:"file,omitempty"`
	Managed    *ManagedSpec    `json:"managed,omitempty"`
	Kubernetes *KubernetesSpec `json:"kubernetes,omitempty"`
	Schedule   string          `json:"schedule,omitempty"`
	Filter     *Filter         `json:"filter,omitempty"`
}

// ViewDefinition is a named, ordered set of source names. Names and origins are immutable.
type ViewDefinition struct {
	Name    string   `json:"name"`
	Origin  Origin   `json:"origin,omitempty"`
	Sources []string `json:"sources"`
}

// Sources exposes reads for both API- and CONFIG-owned definitions (with Origin
// identifying the owner). Create, Update, and Delete mutate API-owned definitions
// only. Create requires empty ID/Origin; Update accepts empty identity metadata or
// the values returned by Get, provided they match the stored identity and API ownership.
type Sources interface {
	CreateSource(context.Context, SourceDefinition) (SourceDefinition, error)
	GetSource(context.Context, string) (SourceDefinition, error)
	ListSources(context.Context) ([]SourceDefinition, error)
	UpdateSource(context.Context, string, SourceDefinition) (SourceDefinition, error)
	DeleteSource(context.Context, string) error
}

// Views exposes reads for both API- and CONFIG-owned named views (with Origin
// identifying the owner). Create, Update, and Delete mutate API-owned views only.
type Views interface {
	CreateView(context.Context, ViewDefinition) (ViewDefinition, error)
	GetView(context.Context, string) (ViewDefinition, error)
	ListViews(context.Context) ([]ViewDefinition, error)
	UpdateView(context.Context, string, ViewDefinition) (ViewDefinition, error)
	DeleteView(context.Context, string) error
}

// Reconciler atomically replaces the CONFIG-owned definition set.
type Reconciler interface {
	// Reconcile replaces the complete CONFIG-owned set atomically. Input origins/IDs
	// must be empty; API-owned names collide rather than being adopted.
	Reconcile(context.Context, []SourceDefinition, []ViewDefinition) error
}

// Definitions combines only the source, view and reconciliation capabilities.
type Definitions interface {
	Sources
	Views
	Reconciler
}

const managedKind = "managed"

var sourceName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

// Kind validates the typed source specification and returns its storage kind.
//
//nolint:gocyclo // A single validation gate covers the five distinct typed spec blocks.
func (s SourceDefinition) Kind() (string, error) {
	if !sourceName.MatchString(s.Name) {
		return "", fmt.Errorf("%w: source name", ErrInvalid)
	}
	count := 0
	kind := ""
	for _, spec := range []struct {
		present bool
		kind    string
	}{
		{s.Git != nil, "git"}, {s.API != nil, "api"}, {s.File != nil, "file"},
		{s.Managed != nil, managedKind}, {s.Kubernetes != nil, "kubernetes"},
	} {
		if spec.present {
			count++
			kind = spec.kind
		}
	}
	if count != 1 {
		return "", fmt.Errorf("%w: exactly one source spec required", ErrInvalid)
	}
	if s.Git != nil {
		if s.Git.Repository == "" {
			return "", fmt.Errorf("%w: git repository required", ErrInvalid)
		}
		refs := 0
		for _, ref := range []string{s.Git.Branch, s.Git.Tag, s.Git.Commit} {
			if ref != "" {
				refs++
			}
		}
		if refs > 1 {
			return "", fmt.Errorf("%w: git ref is ambiguous", ErrInvalid)
		}
		if s.Git.Auth != nil && ((s.Git.Auth.Username == "") != (s.Git.Auth.PasswordFile == "")) {
			return "", fmt.Errorf("%w: git auth requires username and password file", ErrInvalid)
		}
		if s.Git.Auth != nil && s.Git.Auth.PasswordFile != "" && !filepath.IsAbs(s.Git.Auth.PasswordFile) {
			return "", fmt.Errorf("%w: password file must be absolute", ErrInvalid)
		}
	}
	if s.API != nil {
		if s.API.Endpoint == "" {
			return "", fmt.Errorf("%w: api endpoint required", ErrInvalid)
		}
		if s.API.Timeout != "" {
			d, e := time.ParseDuration(s.API.Timeout)
			if e != nil || d <= 0 || d > 5*time.Minute {
				return "", fmt.Errorf("%w: api timeout", ErrInvalid)
			}
		}
	}
	if s.File != nil {
		n := 0
		for _, v := range []string{s.File.Path, s.File.URL, s.File.Data} {
			if v != "" {
				n++
			}
		}
		if n != 1 {
			return "", fmt.Errorf("%w: file needs exactly one path, URL or data", ErrInvalid)
		}
		if s.File.URL != "" {
			u, e := url.Parse(s.File.URL)
			if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
				return "", fmt.Errorf("%w: file URL must be absolute HTTP(S) with host", ErrInvalid)
			}
		}
		if s.File.Timeout != "" {
			d, e := time.ParseDuration(s.File.Timeout)
			if s.File.URL == "" || e != nil || d <= 0 {
				return "", fmt.Errorf("%w: file timeout", ErrInvalid)
			}
		}
	}
	if kind == managedKind || kind == "kubernetes" || (s.File != nil && s.File.Data != "") {
		if s.Schedule != "" || s.Filter != nil {
			return "", fmt.Errorf("%w: non-synced source cannot have schedule/filter", ErrInvalid)
		}
	} else {
		d, e := time.ParseDuration(s.Schedule)
		if e != nil || d <= 0 || d%time.Microsecond != 0 {
			return "", fmt.Errorf("%w: schedule must be positive microsecond-resolution duration", ErrInvalid)
		}
	}
	return kind, nil
}

// ValidateView rejects duplicate and empty references; existence is checked by the backend.
func ValidateView(v ViewDefinition) error {
	if v.Name == "" || len(v.Sources) == 0 {
		return fmt.Errorf("%w: view name and sources required", ErrInvalid)
	}
	seen := make(map[string]bool, len(v.Sources))
	for _, n := range v.Sources {
		if n == "" || seen[n] {
			return fmt.Errorf("%w: empty or duplicate source reference", ErrInvalid)
		}
		seen[n] = true
	}
	return nil
}

// ValidateReconcile validates the whole configuration before any database mutation.
func ValidateReconcile(sources []SourceDefinition, views []ViewDefinition) error {
	names := make(map[string]bool, len(sources))
	managed := 0
	for _, s := range sources {
		if s.ID != "" || s.Origin != "" {
			return fmt.Errorf("%w: input identity/origin must be empty", ErrInvalid)
		}
		kind, e := s.Kind()
		if e != nil {
			return e
		}
		if names[s.Name] {
			return fmt.Errorf("%w: duplicate source", ErrInvalid)
		}
		names[s.Name] = true
		if kind == managedKind {
			managed++
		}
	}
	if managed > 1 {
		return fmt.Errorf("%w: only one managed source", ErrConflict)
	}
	vn := make(map[string]bool, len(views))
	for _, v := range views {
		if v.Origin != "" {
			return fmt.Errorf("%w: input origin must be empty", ErrInvalid)
		}
		if e := ValidateView(v); e != nil {
			return e
		}
		if vn[v.Name] {
			return fmt.Errorf("%w: duplicate view", ErrInvalid)
		}
		vn[v.Name] = true
		for _, n := range v.Sources {
			if !names[n] {
				return fmt.Errorf("%w: config view references source outside config set", ErrInvalid)
			}
		}
	}
	return nil
}
