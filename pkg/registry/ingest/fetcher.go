// Package ingest provides claims-free registry source adapters. Definitions and
// transport access are trusted host inputs, not end-user URL/file fetch endpoints.
package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	gitclient "github.com/stacklok/toolhive-registry-server/internal/git"
	"github.com/stacklok/toolhive-registry-server/internal/httpclient"
	"github.com/stacklok/toolhive-registry-server/internal/shutdown"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// ErrFetch deliberately omits remote responses, URLs, paths and credentials
// from its message. Fetch attaches the redacted cause with WithCause, for logs.
var ErrFetch = errors.New("source fetch failed")

//go:generate mockgen -destination=mocks/io.go -package=mocks -source=fetcher.go

// HTTPClient retrieves a complete bounded response. Implementations must honor
// context cancellation, reject non-success responses, enforce their URL policy
// and apply their own per-request timeout.
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

// Result contains a complete snapshot accepted by its source's policy (see
// Fetch) and an unfiltered change hash. Storage safety is checked later, on
// the filtered snapshot, by persistence.ValidateStorableSnapshot.
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

// Fetch applies each source type's acceptance policy before returning data:
//   - file and Git: the upstream registry format checked against the
//     toolhive-core wire schema, at least one server, and a name and
//     description on every server;
//   - API: the upstream MCP Registry v0.1 pagination protocol only, with no
//     schema check; zero servers is a valid (empty) catalog.
//
// Timeouts apply per HTTP request (api.timeout, file.timeout), not to the whole
// fetch; a long fetch keeps its job lease through the coordinator's renewal.
// Git and file hashes cover original bytes; API hashes cover Data (not its
// generated timestamp). Errors are ErrFetch or persistence.ErrInvalid (or the
// context's error) with the redacted cause attached for logs (see Cause).
func (f *Fetcher) Fetch(ctx context.Context, source persistence.SourceDefinition) (Result, error) {
	kind, err := source.Kind()
	if err != nil {
		return Result{}, err
	}
	switch kind {
	case "api":
		return f.fetchAPI(ctx, source)
	case "git", "file":
	default:
		return Result{}, persistence.ErrInvalid
	}
	data, err := f.read(ctx, source)
	if err != nil {
		return Result{}, fetchError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	snapshot, err := formats.DecodeUpstream(data)
	if err != nil {
		return Result{}, WithCause(persistence.ErrInvalid, err)
	}
	return Result{Snapshot: *snapshot, Hash: fmt.Sprintf("%x", sha256.Sum256(data))}, nil
}

func (f *Fetcher) fetchAPI(ctx context.Context, source persistence.SourceDefinition) (Result, error) {
	upstream, err := NewUpstream(f.client(source))
	if err != nil {
		return Result{}, err
	}
	if err := upstream.Validate(ctx, source.API.Endpoint); err != nil {
		return Result{}, fetchError(ctx, err)
	}
	snapshot, err := upstream.Fetch(ctx, source.API.Endpoint)
	if err != nil {
		return Result{}, fetchError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	slog.InfoContext(ctx, "Fetched all servers from upstream API", "source", source.Name, "count", len(snapshot.Data.Servers))
	hashData, err := json.Marshal(snapshot.Data)
	if err != nil {
		return Result{}, WithCause(persistence.ErrInvalid, err)
	}
	return Result{Snapshot: snapshot, Hash: fmt.Sprintf("%x", sha256.Sum256(hashData))}, nil
}

// client returns the injected HTTP client, or one whose timeout applies to
// each request: api.timeout (default 10s) or file.timeout (default 30s).
func (f *Fetcher) client(s persistence.SourceDefinition) HTTPClient {
	if f.options.HTTP != nil {
		return f.options.HTTP
	}
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
	return httpclient.NewDefaultClient(timeout)
}

func (f *Fetcher) read(ctx context.Context, s persistence.SourceDefinition) ([]byte, error) {
	if s.File == nil {
		return f.readGit(ctx, s.Name, *s.Git)
	}
	if s.File.Data != "" {
		return []byte(s.File.Data), nil
	}
	if s.File.URL != "" {
		return f.client(s).Get(ctx, s.File.URL)
	}
	return f.options.ReadFile(ctx, s.File.Path)
}

func (f *Fetcher) readGit(ctx context.Context, source string, spec persistence.GitSpec) ([]byte, error) {
	password := ""
	if spec.Auth != nil && spec.Auth.PasswordFile != "" {
		if f.options.LoadSecret == nil {
			return nil, errors.New("authenticated Git source has no secret loader")
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
	return f.options.Git.Read(withSourceName(ctx, source), spec, password)
}

// fetchError keeps the returned error's message free of transport detail: it
// is the context's error, context.Canceled/DeadlineExceeded found in err, and
// ErrFetch for any other failure (persistence.ErrInvalid for a rejected
// definition). The redacted cause is attached for logs (see Cause).
func fetchError(ctx context.Context, err error) error {
	if errors.Is(err, persistence.ErrInvalid) {
		return WithCause(persistence.ErrInvalid, err)
	}
	result := ctx.Err()
	if errors.Is(err, context.Canceled) {
		result = errors.Join(result, context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		result = errors.Join(result, context.DeadlineExceeded)
	}
	if result == nil || shutdown.Result(err) != nil {
		result = errors.Join(result, ErrFetch)
	}
	return WithCause(result, err)
}

type sourceNameKey struct{}

func withSourceName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, sourceNameKey{}, name)
}

type defaultGit struct{}

func (defaultGit) Read(ctx context.Context, spec persistence.GitSpec, password string) ([]byte, error) {
	cfg := &gitclient.CloneConfig{URL: spec.Repository, Branch: spec.Branch, Tag: spec.Tag, Commit: spec.Commit}
	if spec.Auth != nil {
		cfg.Auth = &gitclient.AuthConfig{Username: spec.Auth.Username, Password: password}
	}
	source, _ := ctx.Value(sourceNameKey{}).(string)
	repository := RedactURLs(spec.Repository)
	slog.InfoContext(ctx, "Starting git clone", "source", source, "repository", repository,
		"branch", spec.Branch, "tag", spec.Tag, "commit", spec.Commit)
	client := gitclient.NewDefaultGitClient()
	started := time.Now()
	repo, err := client.Clone(ctx, cfg)
	if err != nil {
		if ctx.Err() == nil {
			slog.ErrorContext(ctx, "Git clone failed", "source", source, "repository", repository,
				"duration", time.Since(started).String(), "error", RedactURLs(err.Error()))
		}
		return nil, err
	}
	defer func() { _ = client.Cleanup(ctx, repo) }()
	attrs := []any{"source", source, "repository", repository, "duration", time.Since(started).String(), "branch", repo.Branch}
	if repo.Repository != nil {
		if ref, headErr := repo.Repository.Head(); headErr == nil {
			attrs = append(attrs, "commit_sha", ref.Hash().String())
		}
	}
	slog.InfoContext(ctx, "Git clone completed", attrs...)
	return client.GetFileContent(repo, spec.Path)
}
