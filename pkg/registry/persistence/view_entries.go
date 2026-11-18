package persistence

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

// EntryName identifies a name within a view. Namespace is a payload filter for
// skills/plugins, never part of the (kind,name) source ownership key.
type EntryName struct {
	Kind      EntryKind
	Name      string
	Namespace string
}

// ViewQuery selects versions after whole-name source priority has been resolved.
// An empty VersionSelector selects all versions; Exact and Latest are exclusive.
// Search is a case-insensitive literal substring of name, title or description.
// UpdatedSince is exclusive. Namespace is supported only for skills/plugins.
// Results are ordered by name then exact raw version, ascending byte-lexically.
// Limit must be 1..1000. Cursors are backend-private and at most MaxCursorBytes.
type ViewQuery struct {
	Kind            EntryKind
	Name            string
	Namespace       string
	Search          string
	VersionSelector VersionSelector
	UpdatedSince    *time.Time
	Limit           int
	Cursor          string
}

// ViewEntryReader reads raw, non-personalized named views. It is not an
// authorization boundary. Each call reads view identity, ordered source
// incarnations and entries coherently; pages do not share a frozen snapshot.
// The first linked source with versions wins the whole (kind,name), before ALL
// filters, latest selection and pagination. Losing sources never fill gaps.
// Missing views and Get misses return ErrNotFound; an existing empty list does not.
// Invalid queries/cursors return ErrInvalid; backend/payload failures return
// ErrUnavailable without backend details. Context cancellation is preserved.
type ViewEntryReader interface {
	ListViewEntries(context.Context, string, ViewQuery) (EntryPage, error)
	GetViewEntry(context.Context, string, EntryName, VersionSelector) (Entry, error)
}

// Validate checks a bounded list query, without performing IO.
func (q ViewQuery) Validate(viewName string) error {
	if viewName == "" || q.Limit < 1 || q.Limit > 1000 || len(q.Cursor) > MaxCursorBytes ||
		q.VersionSelector.Latest && q.VersionSelector.Exact != "" ||
		q.Kind == ServerKind && q.Namespace != "" {
		return ErrInvalid
	}
	for _, value := range []string{viewName, q.Name, q.Namespace, q.Search, q.VersionSelector.Exact} {
		if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return ErrInvalid
		}
	}
	if q.UpdatedSince != nil {
		if _, err := q.UpdatedSince.UTC().MarshalJSON(); err != nil {
			return ErrInvalid
		}
	}
	return q.Kind.Validate()
}

// Validate checks an exact name lookup, without interpreting literal "latest".
func (n EntryName) Validate(viewName string, selector VersionSelector) error {
	if n.Name == "" || selector.Latest == (selector.Exact != "") {
		return ErrInvalid
	}
	return (ViewQuery{Kind: n.Kind, Name: n.Name, Namespace: n.Namespace, VersionSelector: selector, Limit: 1}).Validate(viewName)
}
