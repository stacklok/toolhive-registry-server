package persistence

import (
	"context"
	"fmt"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
)

// EntryKind identifies the entry table, independent of a source's kind.
type EntryKind string

// ServerKind, SkillKind, and PluginKind are the three persisted entry kinds.
const (
	ServerKind EntryKind = "MCP"
	SkillKind  EntryKind = "SKILL"
	PluginKind EntryKind = "PLUGIN"
)

// Entry is a raw source-owned catalog version, not a visibility decision.
// Exactly one of Server, Skill, and Plugin is populated. ID is a stable,
// opaque version identifier; SourceID identifies the source incarnation.
type Entry struct {
	ID       string
	SourceID string
	Kind     EntryKind
	Name     string
	Version  string
	IsLatest bool
	Server   *model.Server
	Skill    *model.Skill
	Plugin   *model.Plugin
}

// VersionSelector distinguishes an exact literal version (including "latest")
// from the latest version under model.CompareVersions.
type VersionSelector struct {
	Exact  string
	Latest bool
}

// ListOptions scopes reads to one source incarnation and one entry kind.
// Search is a case-insensitive literal substring of name, title or description.
// Limit is between 1 and 100; Cursor binds the source, kind and all filters.
type ListOptions struct {
	SourceID   string
	Kind       EntryKind
	Name       string
	Search     string
	LatestOnly bool
	Limit      int
	Cursor     string
}

// EntryPage contains one bounded, ordered page and an opaque continuation.
type EntryPage struct {
	Entries    []Entry
	NextCursor string
}

// MaxCursorBytes is the largest accepted or emitted opaque entry cursor.
const MaxCursorBytes = 4096

// EntryReader exposes raw catalog data. A host must authorize calls externally.
type EntryReader interface {
	GetEntry(context.Context, string, EntryKind, string, VersionSelector) (Entry, error)
	ListEntries(context.Context, ListOptions) (EntryPage, error)
}

// SnapshotWriter atomically replaces one non-managed source's complete catalog.
// The source ID prevents delete/recreate from retargeting an old fetch. This
// unleased path rejects an active job lease and invalidates previously issued
// tokens on success; use Jobs.CommitSnapshot for fenced writes and atomic ack.
type SnapshotWriter interface {
	ReplaceSnapshot(context.Context, SourceDefinition, model.Snapshot) error
}

// ManagedPublisher writes individual versions into the singleton managed source.
// It never replaces the managed source's entire history.
type ManagedPublisher interface {
	Publish(context.Context, Entry) (Entry, error)
	DeleteManaged(context.Context, EntryKind, string, string) error
}

// Entries groups the independent reader, snapshot and managed capabilities.
type Entries interface {
	EntryReader
	SnapshotWriter
	ManagedPublisher
}

// Validate rejects entry kinds outside the persisted catalog enum.
func (k EntryKind) Validate() error {
	if k != ServerKind && k != SkillKind && k != PluginKind {
		return fmt.Errorf("%w: unknown entry kind", ErrInvalid)
	}
	return nil
}
