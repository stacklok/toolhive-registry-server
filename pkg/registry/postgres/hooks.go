package postgres

import (
	"context"
	"errors"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// EntryWriteOp names the entry write an EntryWriteHook observes.
type EntryWriteOp string

// Entry writes that run hooks.
const (
	// EntryWriteSnapshot replaces every entry of a synced source
	// (Entries.ReplaceSnapshot and Jobs.CommitSnapshot).
	EntryWriteSnapshot EntryWriteOp = "snapshot"
	// EntryWritePublish adds one managed version (Entries.Publish).
	EntryWritePublish EntryWriteOp = "publish"
	// EntryWriteDelete removes one managed version (Entries.DeleteManaged).
	EntryWriteDelete EntryWriteOp = "delete"
)

// EntryWrite describes one entry write. SourceID is the written source.
// Snapshot is set for EntryWriteSnapshot; Entry for EntryWritePublish; Kind,
// Name and Version for EntryWritePublish and EntryWriteDelete. Hooks must not
// modify the referenced values.
type EntryWrite struct {
	Op         EntryWriteOp
	SourceID   string
	SourceName string
	Snapshot   *model.Snapshot
	Entry      *persistence.Entry
	Kind       persistence.EntryKind
	Name       string
	Version    string
}

// EntryWriteHook lets a host keep its own per-entry data consistent with
// adapter writes, atomically. Both methods run inside the adapter's
// serializable write transaction: BeforeEntryWrite after the adapter has
// resolved and checked the source but before it changes any entry row, and
// AfterEntryWrite after the entry rows are written but before commit.
// BeforeEntryWrite runs before the adapter's own entry checks (the duplicate
// version check of a publish, the existence check of a delete), so a host
// check decides which error a caller sees first.
//
// A non-nil error aborts the write and is returned to the caller unchanged,
// except that a PostgreSQL serialization failure or deadlock retries the
// whole transaction. Because of those retries, a hook can run more than once
// for one call and must only act through tx. The adapter never inspects or
// writes the host's data; the hook owns its own columns and tables.
type EntryWriteHook interface {
	BeforeEntryWrite(ctx context.Context, tx pgx.Tx, write EntryWrite) error
	AfterEntryWrite(ctx context.Context, tx pgx.Tx, write EntryWrite) error
}

// Option configures an Entries or Jobs adapter.
type Option func(*options)

type options struct {
	hook EntryWriteHook
}

// WithEntryWriteHook runs hook around every entry write of the adapter.
func WithEntryWriteHook(hook EntryWriteHook) Option {
	return func(o *options) { o.hook = hook }
}

func applyOptions(opts []Option) (options, error) {
	var o options
	for _, opt := range opts {
		if opt == nil {
			return options{}, persistence.ErrInvalid
		}
		opt(&o)
	}
	if o.hook != nil {
		if v := reflect.ValueOf(o.hook); v.Kind() == reflect.Pointer && v.IsNil() {
			return options{}, persistence.ErrInvalid
		}
	}
	return o, nil
}

func (o options) before(ctx context.Context, tx pgx.Tx, write EntryWrite) error {
	if o.hook == nil {
		return nil
	}
	return hookResult(o.hook.BeforeEntryWrite(ctx, tx, write))
}

func (o options) after(ctx context.Context, tx pgx.Tx, write EntryWrite) error {
	if o.hook == nil {
		return nil
	}
	return hookResult(o.hook.AfterEntryWrite(ctx, tx, write))
}

// PostgreSQL errors that retry a write transaction.
const (
	pgSerializationFailure = "40001"
	pgDeadlockDetected     = "40P01"
)

// hookError carries a host error through classify unchanged.
type hookError struct{ err error }

func (e hookError) Error() string { return e.err.Error() }
func (e hookError) Unwrap() error { return e.err }

func hookResult(err error) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == pgSerializationFailure || pg.Code == pgDeadlockDetected) {
		return err
	}
	return hookError{err: err}
}
