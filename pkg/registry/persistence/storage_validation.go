package persistence

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
)

// MaxStoredIdentityBytes bounds an entry name or version. The consumer read
// routes reject longer path parameters, so a longer identity could never be
// addressed; the (name, version) btree index also rejects rows near this size.
const MaxStoredIdentityBytes = 4096

// ValidateStorableSnapshot is the storage-safety check for synced data. It
// accepts everything the database and the consumer read routes can hold, and
// nothing more: it deliberately applies no schema, no server name format beyond
// what the routes need, and no required description. Fetchers apply their
// source's own acceptance policy before it (see ingest and operator), and
// Jobs.CommitSnapshot and SnapshotWriter.ReplaceSnapshot apply it again.
//
// Each server, skill and plugin must have
//   - a non-empty name and version of at most MaxStoredIdentityBytes bytes,
//     unique per kind within the snapshot;
//   - a server name of the shape "namespace/name": exactly one '/', both parts
//     non-empty, no whitespace or control characters;
//   - a skill or plugin status, when set, that is one of ACTIVE, DEPRECATED or
//     ARCHIVED in any case (the database enum);
//   - valid UTF-8 without NUL in every string, map key and metadata value, and
//     content that encodes as JSON.
//
// Duplicate packages, remotes and icons within a server are not errors: storage
// keeps the first of each, as the v1.5.4 writer did for remotes. The error
// wraps ErrInvalid and names the record (kind, name, version) and the reason.
func ValidateStorableSnapshot(s *model.Snapshot) error {
	if s == nil {
		return fmt.Errorf("%w: nil snapshot", ErrInvalid)
	}
	seen := make(map[string]bool)
	for i := range s.Data.Servers {
		v := &s.Data.Servers[i]
		if err := storableRecord(seen, ServerKind, v.Name, v.Version, "", v); err != nil {
			return err
		}
	}
	for i := range s.Data.Skills {
		v := &s.Data.Skills[i]
		if err := storableRecord(seen, SkillKind, v.Name, v.Version, v.Status, v); err != nil {
			return err
		}
	}
	for i := range s.Data.Plugins {
		v := &s.Data.Plugins[i]
		if err := storableRecord(seen, PluginKind, v.Name, v.Version, v.Status, v); err != nil {
			return err
		}
	}
	return nil
}

// RecordError reports one invalid snapshot record. It wraps ErrInvalid; its
// message names the record and the reason, which are catalog data, not secrets.
type RecordError struct {
	Kind    EntryKind
	Name    string
	Version string
	Reason  string
}

// Error implements error.
func (e *RecordError) Error() string {
	return fmt.Sprintf("%s: %s %q version %q: %s", ErrInvalid, kindLabel(e.Kind), e.Name, e.Version, e.Reason)
}

// Unwrap makes errors.Is(err, ErrInvalid) hold.
func (*RecordError) Unwrap() error { return ErrInvalid }

func storableRecord(seen map[string]bool, kind EntryKind, name, version, status string, record any) error {
	fail := func(format string, args ...any) error {
		return &RecordError{Kind: kind, Name: name, Version: version, Reason: fmt.Sprintf(format, args...)}
	}
	if name == "" {
		return fail("name is required")
	}
	if version == "" {
		return fail("version is required")
	}
	if len(name) > MaxStoredIdentityBytes || len(version) > MaxStoredIdentityBytes {
		return fail("name and version must each be at most %d bytes", MaxStoredIdentityBytes)
	}
	if path, reason := unstorableString(reflect.ValueOf(record), "", 0); reason != "" {
		if path == "" {
			return fail("%s", reason)
		}
		return fail("%s: %s", path, reason)
	}
	if kind == ServerKind {
		if reason := addressableServerName(name); reason != "" {
			return fail("%s", reason)
		}
	}
	if status != "" {
		switch strings.ToUpper(status) {
		case "ACTIVE", "DEPRECATED", "ARCHIVED":
		default:
			return fail("status must be ACTIVE, DEPRECATED or ARCHIVED")
		}
	}
	if _, err := json.Marshal(record); err != nil {
		return fail("content cannot be encoded as JSON")
	}
	key := string(kind) + "\x00" + name + "\x00" + version
	if seen[key] {
		return fail("duplicate name and version")
	}
	seen[key] = true
	return nil
}

// addressableServerName reports why the consumer routes could not carry name,
// or "" when they can: "namespace/name" with exactly one '/', both parts
// non-empty, and no whitespace or control characters. Unlike
// formats.ValidateServerName it does not restrict the characters of each part.
func addressableServerName(name string) string {
	namespace, rest, ok := strings.Cut(name, "/")
	if !ok || namespace == "" || rest == "" || strings.Contains(rest, "/") {
		return "server name must have the form namespace/name with exactly one '/'"
	}
	if strings.IndexFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return "server name must not contain whitespace or control characters"
	}
	return ""
}

var timeType = reflect.TypeFor[time.Time]()

// maxStorableDepth bounds the walk; JSON-decoded and model content is far
// shallower, so reaching it means a cyclic or pathological value.
const maxStorableDepth = 64

// unstorableString walks every string, map key and dynamic value reachable
// from v and returns the field path and reason for the first one Postgres text
// or jsonb columns would reject (invalid UTF-8 or NUL), or "" when all are fine.
//
//nolint:gocyclo // One reflective walk over every value kind the model can hold.
func unstorableString(v reflect.Value, path string, depth int) (string, string) {
	if depth > maxStorableDepth {
		return path, "content is nested too deeply"
	}
	//nolint:exhaustive // Numbers, booleans and the rest hold no text.
	switch v.Kind() {
	case reflect.String:
		return path, badText(v.String())
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return "", ""
		}
		return unstorableString(v.Elem(), path, depth+1)
	case reflect.Struct:
		if v.Type() == timeType {
			return "", ""
		}
		for i := range v.NumField() {
			field := v.Type().Field(i)
			if !field.IsExported() {
				continue
			}
			if p, reason := unstorableString(v.Field(i), joinPath(path, jsonName(field)), depth+1); reason != "" {
				return p, reason
			}
		}
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
			// Raw JSON (json.RawMessage) or bytes: stored as jsonb, which rejects
			// a \u0000 escape, or encoded as base64 text.
			if json.Valid(v.Bytes()) {
				var decoded any
				if err := json.Unmarshal(v.Bytes(), &decoded); err == nil {
					return unstorableString(reflect.ValueOf(decoded), path, depth+1)
				}
			}
			return "", ""
		}
		for i := range v.Len() {
			if p, reason := unstorableString(v.Index(i), fmt.Sprintf("%s[%d]", path, i), depth+1); reason != "" {
				return p, reason
			}
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			key := iter.Key()
			keyPath := path
			if key.Kind() == reflect.String {
				if reason := badText(key.String()); reason != "" {
					return path, "key " + reason
				}
				keyPath = joinPath(path, key.String())
			}
			if p, reason := unstorableString(iter.Value(), keyPath, depth+1); reason != "" {
				return p, reason
			}
		}
	default:
	}
	return "", ""
}

func badText(s string) string {
	if !utf8.ValidString(s) {
		return "text is not valid UTF-8"
	}
	if strings.ContainsRune(s, 0) {
		return "text contains a NUL character"
	}
	return ""
}

func jsonName(field reflect.StructField) string {
	name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
	if name == "" || name == "-" {
		return field.Name
	}
	return name
}

func joinPath(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func kindLabel(kind EntryKind) string {
	switch kind {
	case ServerKind:
		return "server"
	case SkillKind:
		return "skill"
	case PluginKind:
		return "plugin"
	default:
		return string(kind)
	}
}
