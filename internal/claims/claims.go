// Package claims holds the server's claims-mode authorization rules: how a
// caller's JWT claims are matched against the claims stored on sources,
// registries and entries. The rules are v1.5.4's, unchanged; the library under
// pkg/registry knows nothing about claims.
//
// Two matching rules exist and differ only within an array value:
//   - Visible (read path): OR within arrays. A record tagged team:[eng,data]
//     is visible to a caller in either team.
//   - Contain (write path): AND within arrays. A caller supplying new claims
//     must hold every value, or they could grant wider visibility than their
//     own.
//
// Both share the key-level contract: AND across record keys, the caller must
// hold every record key, an empty-array record value is satisfied by the
// key's presence, and unsupported record values fail closed.
package claims

import (
	"context"
	"encoding/json"

	"github.com/stacklok/toolhive-registry-server/internal/db"
)

// Visible reports whether a caller may see a record using the read-path rule.
func Visible(caller, record map[string]any) bool {
	return match(caller, record, overlaps)
}

// Contain reports whether a caller covers every claim value of a record using
// the write-path rule.
func Contain(caller, record map[string]any) bool {
	return match(caller, record, subsetOf)
}

// Equal reports whether a and b hold exactly the same keys and values.
func Equal(a, b map[string]any) bool {
	return Contain(a, b) && Contain(b, a)
}

// VisibleJSON applies the read path to raw JSON the way the entry filter did
// in v1.5.4: empty caller or record bytes (SQL NULL) are hidden, a record that
// is not a JSON object is hidden, and JSON null or {} records carry no
// requirements and are visible. The SQL function claims_visible must agree
// with it for every input; see the shared case table in this package's tests.
func VisibleJSON(callerJSON, recordJSON []byte) bool {
	if len(callerJSON) == 0 || len(recordJSON) == 0 {
		return false
	}
	var caller, record map[string]any
	if err := json.Unmarshal(callerJSON, &caller); err != nil {
		return false
	}
	if err := json.Unmarshal(recordJSON, &record); err != nil {
		return false
	}
	return Visible(caller, record)
}

// Deserialize decodes stored claims. Empty bytes, invalid JSON and JSON that
// is not an object yield nil, and JSON null yields nil; {} yields an empty map.
func Deserialize(data []byte) map[string]any {
	if len(data) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

// WithViewCaller makes the library's view reads filter entries by claims for
// a caller whose JWT claims marshal to callerJSON; see db.WithViewCaller.
func WithViewCaller(ctx context.Context, callerJSON []byte) context.Context {
	return db.WithViewCaller(ctx, callerJSON)
}

// ViewCaller returns the caller claims set by WithViewCaller, or nil.
func ViewCaller(ctx context.Context) []byte { return db.ViewCaller(ctx) }

func match(caller, record map[string]any, values func(required, have map[string]struct{}) bool) bool {
	for k, rv := range record {
		if !validValue(rv) {
			return false
		}
		cv, ok := caller[k]
		if !ok {
			return false
		}
		required := stringSet(rv)
		if len(required) == 0 {
			continue
		}
		if !values(required, stringSet(cv)) {
			return false
		}
	}
	return true
}

func subsetOf(required, have map[string]struct{}) bool {
	for v := range required {
		if _, ok := have[v]; !ok {
			return false
		}
	}
	return true
}

func overlaps(a, b map[string]struct{}) bool {
	if len(b) < len(a) {
		a, b = b, a
	}
	for v := range a {
		if _, ok := b[v]; ok {
			return true
		}
	}
	return false
}

func validValue(v any) bool {
	switch val := v.(type) {
	case string, []string:
		return true
	case []any:
		for _, elem := range val {
			if _, ok := elem.(string); !ok {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func stringSet(v any) map[string]struct{} {
	switch val := v.(type) {
	case string:
		return map[string]struct{}{val: {}}
	case []any:
		s := make(map[string]struct{}, len(val))
		for _, elem := range val {
			if str, ok := elem.(string); ok {
				s[str] = struct{}{}
			}
		}
		return s
	case []string:
		s := make(map[string]struct{}, len(val))
		for _, str := range val {
			s[str] = struct{}{}
		}
		return s
	default:
		return map[string]struct{}{}
	}
}
