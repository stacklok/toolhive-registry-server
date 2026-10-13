// Package ingest provides claims-free registry source adapters. Definitions and
// transport access are trusted host inputs, not end-user URL/file fetch endpoints.
package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	gitclient "github.com/stacklok/toolhive-registry-server/internal/git"
	"github.com/stacklok/toolhive-registry-server/internal/httpclient"
	"github.com/stacklok/toolhive-registry-server/internal/shutdown"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// ErrFetch deliberately omits remote responses, URLs, paths and credentials.
var ErrFetch = errors.New("source fetch failed")

// HTTPClient retrieves a complete bounded response. Implementations must honor
// context cancellation, reject non-success responses and enforce their URL policy.
type HTTPClient interface {
	Get(context.Context, string) ([]byte, error)
}

// GitReader retrieves one committed file. Implementations must honor cancellation.
type GitReader interface {
	Read(context.Context, persistence.GitSpec, string) ([]byte, error)
}

// Options supplies borrowed dependencies. Nil HTTP/Git/ReadFile use the existing
// bounded HTTP and in-memory Git machinery and os.ReadFile. LoadSecret is required
// for authenticated Git and must resolve/validate the absolute credential reference.
// No supplied client is closed. ReadFile must honor cancellation where supported.
type Options struct {
	HTTP       HTTPClient
	Git        GitReader
	ReadFile   func(context.Context, string) ([]byte, error)
	LoadSecret func(context.Context, string) (string, error)
}

// Result contains a complete validated snapshot and an unfiltered change hash.
type Result struct {
	Snapshot model.Snapshot
	Hash     string
}

// Fetcher can be used directly by a host coordinator or by reconcile.Coordinator.
type Fetcher struct{ options Options }

// New constructs an adapter without I/O, background work or global configuration.
func New(options Options) *Fetcher {
	if options.Git == nil {
		options.Git = defaultGit{}
	}
	if options.ReadFile == nil {
		options.ReadFile = func(ctx context.Context, path string) ([]byte, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			//nolint:gosec // Paths are supplied by the trusted host definition.
			return os.ReadFile(path)
		}
	}
	return &Fetcher{options: options}
}

// Fetch validates before returning any data. Git and file hashes cover original
// bytes; API hashes cover Data (not its generated timestamp). Import formats still
// require a server: an empty/malformed remote response cannot erase the catalog.
func (f *Fetcher) Fetch(ctx context.Context, source persistence.SourceDefinition) (Result, error) {
	kind, err := source.Kind()
	if err != nil {
		return Result{}, err
	}
	if kind != "git" && kind != "api" && kind != "file" {
		return Result{}, persistence.ErrInvalid
	}
	timeout := 5 * time.Minute
	if source.API != nil && source.API.Timeout != "" {
		timeout, _ = time.ParseDuration(source.API.Timeout)
	}
	if source.File != nil && source.File.Timeout != "" {
		timeout, _ = time.ParseDuration(source.File.Timeout)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	data, err := f.read(ctx, source)
	if err != nil {
		return Result{}, safeError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	snapshot, err := formats.DecodeUpstream(data)
	if err != nil {
		return Result{}, persistence.ErrInvalid
	}
	if err := persistence.ValidateSnapshot(snapshot); err != nil {
		return Result{}, err
	}
	hashData := data
	if source.API != nil {
		hashData, err = json.Marshal(snapshot.Data)
		if err != nil {
			return Result{}, persistence.ErrInvalid
		}
	}
	return Result{Snapshot: *snapshot, Hash: fmt.Sprintf("%x", sha256.Sum256(hashData))}, nil
}

func (f *Fetcher) read(ctx context.Context, s persistence.SourceDefinition) ([]byte, error) {
	client := f.options.HTTP
	if client == nil {
		timeout := httpclient.DefaultTimeout
		if s.File != nil {
			timeout = 30 * time.Second
			if s.File.Timeout != "" {
				timeout, _ = time.ParseDuration(s.File.Timeout)
			}
		}
		if s.API != nil && s.API.Timeout != "" {
			timeout, _ = time.ParseDuration(s.API.Timeout)
		}
		client = httpclient.NewDefaultClient(timeout)
	}
	switch {
	case s.File != nil:
		if s.File.Data != "" {
			return []byte(s.File.Data), nil
		}
		if s.File.URL != "" {
			return client.Get(ctx, s.File.URL)
		}
		return f.options.ReadFile(ctx, s.File.Path)
	case s.API != nil:
		upstream, err := NewUpstream(client)
		if err != nil {
			return nil, err
		}
		if err := upstream.Validate(ctx, s.API.Endpoint); err != nil {
			return nil, err
		}
		snapshot, err := upstream.Fetch(ctx, s.API.Endpoint)
		if err != nil {
			return nil, err
		}
		return json.Marshal(snapshot)
	default:
		return f.readGit(ctx, *s.Git)
	}
}

func (f *Fetcher) readGit(ctx context.Context, spec persistence.GitSpec) ([]byte, error) {
	password := ""
	if spec.Auth != nil && spec.Auth.PasswordFile != "" {
		if f.options.LoadSecret == nil {
			return nil, ErrFetch
		}
		var err error
		password, err = f.options.LoadSecret(ctx, spec.Auth.PasswordFile)
		if err != nil {
			return nil, err
		}
	}
	if spec.Path == "" {
		spec.Path = "registry.json"
	}
	return f.options.Git.Read(ctx, spec, password)
}

func safeError(ctx context.Context, err error) error {
	result := ctx.Err()
	if errors.Is(err, context.Canceled) {
		result = errors.Join(result, context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		result = errors.Join(result, context.DeadlineExceeded)
	}
	if shutdown.Result(err) != nil {
		result = errors.Join(result, ErrFetch)
	}
	return result
}

type defaultGit struct{}

func (defaultGit) Read(ctx context.Context, spec persistence.GitSpec, password string) ([]byte, error) {
	cfg := &gitclient.CloneConfig{URL: spec.Repository, Branch: spec.Branch, Tag: spec.Tag, Commit: spec.Commit}
	if spec.Auth != nil {
		cfg.Auth = &gitclient.AuthConfig{Username: spec.Auth.Username, Password: password}
	}
	client := gitclient.NewDefaultGitClient()
	repo, err := client.Clone(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer client.Cleanup(ctx, repo)
	return client.GetFileContent(repo, spec.Path)
}
