package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

//nolint:gocyclo // Route-specific validation and error exits stay at the HTTP boundary.
func serveConsumer(pages ConsumerPages, options ConsumerOptions, route RouteDescriptor, w http.ResponseWriter, r *http.Request) {
	values, err := consumerQuery(r, route, options)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	view, err := pathParam(r, registryNameParam)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if route.Operation == List {
		query, err := consumerListQuery(r, route, options, values)
		if err != nil {
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := query.Validate(view); err != nil {
			writeServiceError(w, err)
			return
		}
		page, err := pages.List(r.Context(), view, query)
		if err != nil {
			writeApplicationError(w, r, route, options.MapError, err)
			return
		}
		writePage(w, route.Kind, page, query.Limit)
		return
	}
	name, err := entryName(r, route.Kind)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if route.Operation == ListVersions {
		request, err := historyRequest(route.Kind, values, options.PaginateHistory)
		if err != nil {
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}
		query := catalog.Query{
			Kind: name.Kind, Name: name.Name, Namespace: name.Namespace, Limit: request.Limit, Cursor: request.Cursor,
		}
		if err := query.Validate(view); err != nil {
			writeServiceError(w, err)
			return
		}
		page, err := pages.ListVersions(r.Context(), view, name, request)
		if err != nil {
			writeApplicationError(w, r, route, options.MapError, err)
			return
		}
		// Legacy server history has no wire continuation.
		if route.Kind == persistence.ServerKind && !options.PaginateHistory {
			page.NextCursor = ""
		}
		writePage(w, route.Kind, page, request.Limit)
		return
	}
	selector := catalog.VersionSelector{Latest: true}
	if route.Operation == Get {
		version, err := pathParam(r, "version")
		if err != nil {
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}
		selector = wireSelector(version)
	}
	if err := name.Validate(view, selector); err != nil {
		writeServiceError(w, err)
		return
	}
	entry, err := pages.Get(r.Context(), view, name, selector)
	if err != nil {
		writeApplicationError(w, r, route, options.MapError, err)
		return
	}
	if !validPayload(route.Kind, entry) {
		writeServiceError(w, persistence.ErrUnavailable)
		return
	}
	switch route.Kind {
	case persistence.ServerKind:
		writeJSON(w, formats.ServerPayload(entry.Server), http.StatusOK)
	case persistence.SkillKind:
		writeJSON(w, formats.SkillPayload(entry.Skill), http.StatusOK)
	case persistence.PluginKind:
		writeJSON(w, formats.PluginPayload(entry.Plugin), http.StatusOK)
	}
}

func pathParam(r *http.Request, param string) (string, error) {
	value, err := url.PathUnescape(chi.URLParam(r, param))
	if err != nil {
		return "", fmt.Errorf("invalid URL encoding in %s", param)
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s cannot be empty", param)
	}
	if strings.ContainsAny(value, " \t\n\r") {
		return "", fmt.Errorf("%s cannot contain whitespace", param)
	}
	if len(value) > 4096 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return "", fmt.Errorf("invalid %s parameter", param)
	}
	return value, nil
}

func entryName(r *http.Request, kind persistence.EntryKind) (catalog.EntryName, error) {
	name := catalog.EntryName{Kind: kind}
	if kind == persistence.ServerKind {
		value, err := pathParam(r, serverNameParam)
		if err != nil {
			return name, err
		}
		name.Name, err = formats.ValidateServerName(value)
		if err != nil {
			return name, fmt.Errorf("invalid server name format: %w", err)
		}
		return name, nil
	}
	var err error
	name.Namespace, err = pathParam(r, "namespace")
	if err != nil {
		return name, err
	}
	name.Name, err = pathParam(r, "name")
	return name, err
}

// listQuery deliberately ignores extension status/version/updated_since: the
// legacy handlers parsed status but never passed it to selection, and the other
// two parameters were unsupported. Lists include all versions, despite Swagger.
//
//nolint:gocyclo // Compatibility requires distinct server/extension query rules.
func listQuery(r *http.Request, kind persistence.EntryKind) (catalog.Query, error) {
	if len(r.URL.RawQuery) > 16384 {
		return catalog.Query{}, fmt.Errorf("query parameters too large")
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return catalog.Query{}, fmt.Errorf("invalid query parameters")
	}
	query := catalog.Query{Kind: kind, Limit: 30, Search: values.Get("search"), Cursor: values.Get("cursor")}
	integerError := "Invalid limit parameter: must be an integer"
	if kind != persistence.ServerKind {
		query.Limit = 50
		query.Search = strings.TrimSpace(query.Search)
		query.Cursor = strings.TrimSpace(query.Cursor)
		integerError = "invalid limit parameter: must be an integer"
	}
	if raw := values.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return query, errors.New(integerError)
		}
		if kind != persistence.ServerKind && (limit < 1 || limit > 100) {
			return query, fmt.Errorf("invalid limit parameter: must be between 1 and 100")
		}
		if limit < 1 {
			//nolint:staticcheck // Match server validation's wire capitalization.
			return query, fmt.Errorf("Invalid limit parameter: must be positive")
		}
		query.Limit = min(limit, 1000)
	}
	if len(query.Cursor) > persistence.MaxCursorBytes || !utf8.ValidString(query.Cursor) || strings.ContainsRune(query.Cursor, 0) {
		return query, fmt.Errorf("invalid cursor parameter")
	}
	if kind == persistence.ServerKind {
		query.VersionSelector = wireSelector(values.Get("version"))
		if raw := values.Get("updated_since"); raw != "" {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				//nolint:staticcheck // Preserve the legacy wire message.
				return query, fmt.Errorf("Invalid updated_since parameter: must be RFC3339 format (e.g., 2025-08-07T13:15:04.280Z)")
			}
			query.UpdatedSince = &parsed
		}
	}
	return query, nil
}

func wireSelector(version string) catalog.VersionSelector {
	if version == "latest" {
		return catalog.VersionSelector{Latest: true}
	}
	return catalog.VersionSelector{Exact: version}
}

func validPayload(kind persistence.EntryKind, entry persistence.Entry) bool {
	if entry.Kind != kind {
		return false
	}
	switch kind {
	case persistence.ServerKind:
		return entry.Server != nil && entry.Skill == nil && entry.Plugin == nil
	case persistence.SkillKind:
		return entry.Skill != nil && entry.Server == nil && entry.Plugin == nil
	case persistence.PluginKind:
		return entry.Plugin != nil && entry.Server == nil && entry.Skill == nil
	default:
		return false
	}
}

func writePage(w http.ResponseWriter, kind persistence.EntryKind, page catalog.Page, limit int) {
	if len(page.Entries) > limit {
		writeServiceError(w, persistence.ErrUnavailable)
		return
	}
	servers := make([]*model.Server, 0, len(page.Entries))
	skills := make([]*model.Skill, 0, len(page.Entries))
	plugins := make([]*model.Plugin, 0, len(page.Entries))
	for _, entry := range page.Entries {
		if !validPayload(kind, entry) {
			writeServiceError(w, persistence.ErrUnavailable)
			return
		}
		switch kind {
		case persistence.ServerKind:
			servers = append(servers, entry.Server)
		case persistence.SkillKind:
			skills = append(skills, entry.Skill)
		case persistence.PluginKind:
			plugins = append(plugins, entry.Plugin)
		}
	}
	switch kind {
	case persistence.ServerKind:
		writeJSON(w, formats.ServerPage(servers, page.NextCursor), http.StatusOK)
	case persistence.SkillKind:
		writeJSON(w, formats.SkillPage(skills, page.NextCursor), http.StatusOK)
	case persistence.PluginKind:
		writeJSON(w, formats.PluginPage(plugins, page.NextCursor), http.StatusOK)
	}
}

func writeApplicationError(w http.ResponseWriter, r *http.Request, route RouteDescriptor,
	mapper func(*http.Request, RouteDescriptor, error) ErrorMapping, err error,
) {
	if mapper == nil {
		writeServiceError(w, err)
		return
	}
	mapping := mapper(r, route, err)
	if mapping.Status == 0 && mapping.Body == nil {
		writeServiceError(w, err)
		return
	}
	if mapping.Status < 400 || mapping.Status > 599 || isNil(mapping.Body) {
		writeError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, mapping.Body, mapping.Status)
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, persistence.ErrInvalid):
		writeError(w, "invalid request", http.StatusBadRequest)
	case errors.Is(err, persistence.ErrNotFound):
		writeError(w, "not found", http.StatusNotFound)
	default:
		writeError(w, "internal server error", http.StatusInternalServerError)
	}
}

func writeError(w http.ResponseWriter, message string, status int) {
	writeJSON(w, ErrorResponse{Error: message}, status)
}

func writeJSON(w http.ResponseWriter, value any, status int) {
	data, err := json.Marshal(value)
	if err != nil {
		status = http.StatusInternalServerError
		data = []byte(`{"error":"internal server error"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(data, '\n'))
}
