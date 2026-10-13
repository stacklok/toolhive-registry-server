package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// MaxAdminBodyBytes bounds source definitions (including inline catalogs) and publication.
const MaxAdminBodyBytes int64 = 4 << 20

// decodeAdminBody inspects actual top-level keys, including escaped spellings,
// before typed decoding. Nested upstream extensions are not security fields.
// Duplicate case-folded top-level keys are rejected, never last-value-wins.
func decodeAdminBody(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxAdminBodyBytes))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return bodyError(w, err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = persistence.ErrInvalid
		}
		return bodyError(w, err)
	}
	keys := json.NewDecoder(bytes.NewReader(raw))
	token, err := keys.Token()
	if err != nil || token != json.Delim('{') {
		return bodyError(w, persistence.ErrInvalid)
	}
	seen := make(map[string]bool)
	for keys.More() {
		token, err := keys.Token()
		if err != nil {
			return bodyError(w, err)
		}
		key, ok := token.(string)
		if !ok {
			return bodyError(w, persistence.ErrInvalid)
		}
		key = foldJSONKey(key)
		if strings.EqualFold(key, "claims") {
			writeError(w, "resource claims are retired", http.StatusBadRequest)
			return false
		}
		if seen[key] {
			return bodyError(w, persistence.ErrInvalid)
		}
		seen[key] = true
		var value json.RawMessage
		if err := keys.Decode(&value); err != nil {
			return bodyError(w, err)
		}
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return bodyError(w, err)
	}
	return true
}

// Match encoding/json's Unicode simple-fold field matching, including long s
// and Kelvin sign, without a quadratic scan of attacker-controlled object keys.
func foldJSONKey(key string) string {
	return strings.Map(func(r rune) rune {
		minimum := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			minimum = min(minimum, next)
		}
		return minimum
	}, key)
}

func bodyError(w http.ResponseWriter, err error) bool {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, "request body too large", http.StatusRequestEntityTooLarge)
	} else {
		writeError(w, "invalid request body", http.StatusBadRequest)
	}
	return false
}

// ErrorResponse is the JSON error envelope used by the public HTTP adapters.
type ErrorResponse struct {
	Error string `json:"error"`
}

// CommittedErrorResponse reports a persisted mutation whose follow-up failed.
type CommittedErrorResponse struct {
	Error     string `json:"error"`
	Committed bool   `json:"committed"`
	Resource  any    `json:"resource,omitempty"`
}

// SourcesResponse lists source definitions and their allowlisted status.
type SourcesResponse struct {
	Sources []sourceInfo `json:"sources"`
}

// ViewsResponse lists named views.
type ViewsResponse struct {
	Registries []viewInfo `json:"registries"`
}

// ViewRequest specifies ordered source references without resource claims.
type ViewRequest struct {
	Sources []string `json:"sources"`
}

// SourceEntriesResponse lists unshadowed source version diagnostics.
type SourceEntriesResponse struct {
	Entries []sourceEntryInfo `json:"entries"`
}

// ViewEntriesResponse lists unshadowed view version diagnostics.
type ViewEntriesResponse struct {
	Entries []viewEntryInfo `json:"entries"`
}

type sourceRequest struct {
	Git        *persistence.GitSpec     `json:"git,omitempty"`
	API        *persistence.APISpec     `json:"api,omitempty"`
	File       *persistence.FileSpec    `json:"file,omitempty"`
	Managed    *persistence.ManagedSpec `json:"managed,omitempty"`
	Kubernetes *kubernetesConfig        `json:"kubernetes,omitempty"`
	SyncPolicy *struct {
		Interval string `json:"interval"`
	} `json:"syncPolicy,omitempty"`
	Filter *persistence.Filter `json:"filter,omitempty"`
}

// Retain the legacy capitalized wire key explicitly for runtime and OpenAPI parity.
type kubernetesConfig struct {
	Namespaces []string `json:"Namespaces"`
}

func (req sourceRequest) definition(name string) persistence.SourceDefinition {
	s := persistence.SourceDefinition{Name: name, Git: req.Git, API: req.API, File: req.File,
		Managed: req.Managed, Filter: req.Filter}
	if req.Kubernetes != nil {
		s.Kubernetes = &persistence.KubernetesSpec{Namespaces: req.Kubernetes.Namespaces}
	}
	if req.SyncPolicy != nil {
		s.Schedule = req.SyncPolicy.Interval
	}
	return s
}

type sourceInfo struct {
	Name         string              `json:"name"`
	Type         string              `json:"type"`
	CreationType persistence.Origin  `json:"creationType,omitempty"`
	SourceType   string              `json:"sourceType,omitempty"`
	SourceConfig any                 `json:"sourceConfig,omitempty"`
	FilterConfig *persistence.Filter `json:"filterConfig,omitempty"`
	SyncSchedule string              `json:"syncSchedule,omitempty"`
	SyncStatus   *sourceSyncStatus   `json:"syncStatus,omitempty"`
	CreatedAt    time.Time           `json:"createdAt"`
	UpdatedAt    time.Time           `json:"updatedAt"`
}

type sourceSyncStatus struct {
	Phase        string     `json:"phase"`
	LastSyncTime *time.Time `json:"lastSyncTime,omitempty"`
	LastAttempt  *time.Time `json:"lastAttempt,omitempty"`
	AttemptCount int64      `json:"attemptCount"`
	ServerCount  int64      `json:"serverCount"`
	SkillCount   int64      `json:"skillCount"`
	PluginCount  int64      `json:"pluginCount"`
	Message      string     `json:"message,omitempty"`
}

func sourceWire(s persistence.SourceDefinition) sourceInfo {
	info := sourceInfo{Name: s.Name, CreationType: s.Origin, FilterConfig: s.Filter,
		SyncSchedule: s.Schedule, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}
	switch {
	case s.Git != nil:
		info.Type, info.SourceConfig = "git", s.Git
	case s.API != nil:
		info.Type, info.SourceConfig = "api", s.API
	case s.File != nil:
		info.Type, info.SourceConfig = "file", s.File
	case s.Managed != nil:
		info.Type, info.SourceConfig = "managed", s.Managed
	case s.Kubernetes != nil:
		info.Type, info.SourceConfig = "kubernetes", &kubernetesConfig{Namespaces: s.Kubernetes.Namespaces}
	}
	info.SourceType = info.Type
	return info
}

// Only allowlisted status fields are serialized; never marshal JobStatus itself.
func statusWire(s persistence.JobStatus) *sourceSyncStatus {
	phase := "unknown"
	switch s.Phase {
	case persistence.JobPending:
		phase = "pending"
	case persistence.JobCompleted:
		phase = "complete"
	case persistence.JobInProgress:
		phase = "syncing"
	case persistence.JobFailed:
		phase = "failed"
	}
	message := ""
	if persistence.ValidateFailureCategory(s.Failure) == nil {
		message = string(s.Failure)
	}
	return &sourceSyncStatus{Phase: phase, LastSyncTime: s.EndedAt, LastAttempt: s.StartedAt,
		AttemptCount: s.Attempts, ServerCount: s.ServerCount, SkillCount: s.SkillCount,
		PluginCount: s.PluginCount, Message: message}
}

type viewInfo struct {
	Name         string             `json:"name"`
	CreationType persistence.Origin `json:"creationType,omitempty"`
	Sources      []string           `json:"sources"`
	CreatedAt    time.Time          `json:"createdAt"`
	UpdatedAt    time.Time          `json:"updatedAt"`
}

func viewWire(v persistence.ViewDefinition) viewInfo {
	return viewInfo{Name: v.Name, CreationType: v.Origin, Sources: v.Sources, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

type versionInfo struct {
	Version     string    `json:"version"`
	Title       string    `json:"title,omitempty"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type sourceEntryInfo struct {
	EntryType persistence.EntryKind `json:"entryType"`
	Name      string                `json:"name"`
	Versions  []versionInfo         `json:"versions"`
}

type viewEntryInfo struct {
	EntryType persistence.EntryKind `json:"entryType"`
	Name      string                `json:"name"`
	versionInfo
	SourceName string `json:"sourceName"`
	Position   int32  `json:"position"`
}

func versionWire(v persistence.DiagnosticVersion) versionInfo {
	return versionInfo{Version: v.Version, Title: v.Title, Description: v.Description,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

func sourceEntriesWire(rows []persistence.DiagnosticVersion) []sourceEntryInfo {
	entries := make([]sourceEntryInfo, 0)
	positions := make(map[struct {
		kind persistence.EntryKind
		name string
	}]int)
	for _, row := range rows {
		key := struct {
			kind persistence.EntryKind
			name string
		}{row.Kind, row.Name}
		position, found := positions[key]
		if !found {
			position = len(entries)
			positions[key] = position
			entries = append(entries, sourceEntryInfo{EntryType: row.Kind, Name: row.Name, Versions: []versionInfo{}})
		}
		entries[position].Versions = append(entries[position].Versions, versionWire(row))
	}
	return entries
}

type publishRequest struct {
	Server *model.Server `json:"server,omitempty"`
	Skill  *model.Skill  `json:"skill,omitempty"`
	Plugin *model.Plugin `json:"plugin,omitempty"`
}

func (req publishRequest) entry() (persistence.Entry, error) {
	e := persistence.Entry{Server: req.Server, Skill: req.Skill, Plugin: req.Plugin}
	switch {
	case req.Server != nil:
		e.Kind, e.Name, e.Version = persistence.ServerKind, req.Server.Name, req.Server.Version
	case req.Skill != nil:
		e.Kind, e.Name, e.Version = persistence.SkillKind, req.Skill.Name, req.Skill.Version
	case req.Plugin != nil:
		e.Kind, e.Name, e.Version = persistence.PluginKind, req.Plugin.Name, req.Plugin.Version
	}
	return e, e.Validate()
}

// Publication returns the legacy raw payload (not the consumer server envelope).
func publishedWire(e persistence.Entry) any {
	switch e.Kind {
	case persistence.ServerKind:
		return e.Server
	case persistence.SkillKind:
		return e.Skill
	case persistence.PluginKind:
		return e.Plugin
	default:
		return nil
	}
}
