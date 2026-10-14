package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// Legacy detail/history queries are intentionally not parsed unless opted in.
//
//nolint:gocyclo // Keep the complete route-specific query inventory at the boundary.
func consumerQuery(r *http.Request, route RouteDescriptor, options ConsumerOptions) (url.Values, error) {
	if !options.StrictQueries && (!options.PaginateHistory || route.Operation != ListVersions) &&
		(!options.AllowExactName || route.Operation != List) {
		return nil, nil
	}
	if len(r.URL.RawQuery) > 16384 {
		return nil, fmt.Errorf("query parameters too large")
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("invalid query parameters")
	}
	if !options.StrictQueries {
		return values, nil
	}
	for key, items := range values {
		allowed := false
		switch route.Operation {
		case List:
			allowed = key == "limit" || key == "cursor" || key == "search" ||
				key == "name" && options.AllowExactName ||
				route.Kind == persistence.ServerKind && (key == "version" || key == "updated_since" && options.AllowUpdatedSince)
		case ListVersions:
			allowed = options.PaginateHistory && (key == "limit" || key == "cursor")
		case Get, GetLatest:
			allowed = false
		}
		if !allowed || len(items) != 1 || items[0] == "" ||
			!utf8.ValidString(items[0]) || strings.ContainsRune(items[0], 0) {
			return nil, fmt.Errorf("invalid query parameters")
		}
	}
	return values, nil
}

//nolint:gocyclo // Opt-ins must preserve the independent legacy list behavior.
func consumerListQuery(
	r *http.Request, route RouteDescriptor, options ConsumerOptions, values url.Values,
) (catalog.Query, error) {
	query, err := listQuery(r, route.Kind)
	if err != nil {
		return query, err
	}
	if options.StrictQueries {
		query.Search, query.Cursor = values.Get("search"), values.Get("cursor")
		query.Limit, err = controlledLimit(values, route.Kind, query.Limit)
		if err != nil {
			return query, err
		}
		if err := validateCursor(query.Cursor); err != nil {
			return query, err
		}
		if version := values.Get("version"); version != "" && strings.IndexFunc(version, unicode.IsSpace) >= 0 {
			return query, fmt.Errorf("invalid version parameter")
		}
	}
	if options.AllowExactName {
		if items, present := values["name"]; present {
			if len(items) != 1 {
				return query, fmt.Errorf("invalid name parameter")
			}
			name := items[0]
			if name == "" || len(name) > 4096 || !utf8.ValidString(name) ||
				strings.ContainsRune(name, 0) || strings.IndexFunc(name, unicode.IsSpace) >= 0 {
				return query, fmt.Errorf("invalid name parameter")
			}
			if route.Kind == persistence.ServerKind {
				if _, err := formats.ValidateServerName(name); err != nil {
					return query, fmt.Errorf("invalid name parameter")
				}
			}
			query.Name = name
		}
	}
	return query, nil
}

func historyRequest(kind persistence.EntryKind, values url.Values, paginate bool) (catalog.PageRequest, error) {
	request := catalog.PageRequest{Limit: 30}
	if kind == persistence.ServerKind {
		request.Limit = 1000
	}
	if !paginate {
		return request, nil
	}
	var err error
	request.Limit, err = controlledLimit(values, kind, request.Limit)
	if err != nil {
		return request, err
	}
	if items, present := values["cursor"]; present {
		if len(items) != 1 || items[0] == "" {
			return request, fmt.Errorf("invalid cursor parameter")
		}
		request.Cursor = items[0]
	}
	return request, validateCursor(request.Cursor)
}

func controlledLimit(values url.Values, kind persistence.EntryKind, fallback int) (int, error) {
	items, present := values["limit"]
	if !present {
		return fallback, nil
	}
	if len(items) != 1 || items[0] == "" || strings.IndexFunc(items[0], func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return 0, fmt.Errorf("invalid limit parameter")
	}
	limit, err := strconv.Atoi(items[0])
	maximum := 1000
	if kind != persistence.ServerKind {
		maximum = 100
	}
	if err != nil || limit < 1 || limit > maximum {
		return 0, fmt.Errorf("invalid limit parameter: must be between 1 and %d", maximum)
	}
	return limit, nil
}

func validateCursor(cursor string) error {
	if len(cursor) > persistence.MaxCursorBytes || !utf8.ValidString(cursor) || strings.ContainsRune(cursor, 0) {
		return fmt.Errorf("invalid cursor parameter")
	}
	return nil
}
