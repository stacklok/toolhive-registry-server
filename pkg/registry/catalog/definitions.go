package catalog

import (
	"context"
	"errors"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

//go:generate mockgen -destination=mocks/definitions.go -package=mocks github.com/stacklok/toolhive-registry-server/pkg/registry/persistence Sources,Views

// DefinitionAdmin exposes source/view application operations without configuration
// reconciliation, authorization, publication, diagnostics or runtime ownership.
// Reads include both origins; mutations are API-only, as enforced by persistence.
// A host can replace this implementation while retaining these method shapes.
type DefinitionAdmin interface {
	persistence.Sources
	persistence.Views
}

// DefinitionAdminOptions supplies optional, synchronous post-commit work.
// SyncInline is invoked only for a persisted inline file source on create/update.
// NotifyRefresh follows every successful source mutation, even if SyncInline fails.
// Neither callback runs for view mutations or failed persistence operations.
// Callbacks must rediscover current persisted state and respect source fencing;
// they must not share source ownership with legacy writers.
type DefinitionAdminOptions struct {
	NotifyRefresh func(context.Context) error
	SyncInline    func(context.Context, string) error
}

// Post-commit stages can be classified with errors.Is in addition to the original
// callback/context errors. These categories do not mean persistence failed.
var (
	ErrSyncInline    = errors.New("inline synchronization failed")
	ErrNotifyRefresh = errors.New("refresh notification failed")
)

// PostCommitError means the definition mutation committed but follow-up work failed.
// Create/Update also return the stored definition. Delete has already removed it.
// Inspect this type with errors.As BEFORE mapping unwrapped conflict/cancel errors.
// Its message is sanitized; unwrapped callback errors may contain sensitive data.
// There is no compensation or automatic retry: rediscover persisted state before
// deciding whether and how to repeat follow-up work.
type PostCommitError struct {
	// Cause preserves callback errors and errors.Is stage markers. Replacement
	// applications may construct this error after their own committed mutations.
	// Do not log Cause: it can contain sensitive callback details.
	Cause error
}

func (*PostCommitError) Error() string { return "definition committed; lifecycle follow-up failed" }

// Unwrap preserves all failed stages and callback errors, including cancellation.
func (e *PostCommitError) Unwrap() error { return e.Cause }

type definitionAdmin struct {
	persistence.Sources
	persistence.Views
	options DefinitionAdminOptions
}

// NewDefinitionAdmin borrows independently supplied, initialized non-nil backends.
// It performs no IO, starts no work, and does not close or own either dependency.
// Typed nil implementations are outside the caller contract. Missing callbacks
// mean no follow-up work is requested, not that a source has been synchronized.
func NewDefinitionAdmin(
	sources persistence.Sources, views persistence.Views, options DefinitionAdminOptions,
) (DefinitionAdmin, error) {
	if sources == nil || views == nil {
		return nil, persistence.ErrInvalid
	}
	return &definitionAdmin{Sources: sources, Views: views, options: options}, nil
}

func (a *definitionAdmin) CreateSource(
	ctx context.Context, source persistence.SourceDefinition,
) (persistence.SourceDefinition, error) {
	stored, err := a.Sources.CreateSource(ctx, source)
	if err != nil {
		return stored, err
	}
	return stored, a.followUp(ctx, stored)
}

func (a *definitionAdmin) UpdateSource(
	ctx context.Context, name string, source persistence.SourceDefinition,
) (persistence.SourceDefinition, error) {
	stored, err := a.Sources.UpdateSource(ctx, name, source)
	if err != nil {
		return stored, err
	}
	return stored, a.followUp(ctx, stored)
}

func (a *definitionAdmin) DeleteSource(ctx context.Context, name string) error {
	if err := a.Sources.DeleteSource(ctx, name); err != nil {
		return err
	}
	return a.followUp(ctx, persistence.SourceDefinition{})
}

func (a *definitionAdmin) followUp(ctx context.Context, stored persistence.SourceDefinition) error {
	var failures error
	if stored.File != nil && stored.File.Data != "" && a.options.SyncInline != nil {
		if err := a.options.SyncInline(ctx, stored.Name); err != nil {
			failures = errors.Join(ErrSyncInline, err)
		}
	}
	if a.options.NotifyRefresh != nil {
		if err := a.options.NotifyRefresh(ctx); err != nil {
			failures = errors.Join(failures, ErrNotifyRefresh, err)
		}
	}
	if failures != nil {
		return &PostCommitError{Cause: failures}
	}
	return nil
}
