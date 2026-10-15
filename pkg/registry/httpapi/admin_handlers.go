package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

func serveAdmin(ops AdminOperations, route RouteDescriptor, w http.ResponseWriter, r *http.Request) {
	name := ""
	if route.ResourceParam != "" {
		var err error
		name, err = pathParam(r, route.ResourceParam)
		if err != nil {
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	switch route.ResourceType {
	case "source":
		serveSourceAdmin(ops, route, name, w, r)
	case "registry":
		serveViewAdmin(ops, route, name, w, r)
	case entryResourceType:
		serveEntryAdmin(ops.Publisher, route, name, w, r)
	}
}

//nolint:gocyclo,exhaustive // The closed admin inventory excludes consumer-only operations.
func serveSourceAdmin(ops AdminOperations, route RouteDescriptor, name string, w http.ResponseWriter, r *http.Request) {
	switch route.Operation {
	case List:
		sources, err := ops.Sources.ListSources(r.Context())
		if err != nil {
			writeAdminError(w, err)
			return
		}
		infos := make([]sourceInfo, 0, len(sources))
		for _, source := range sources {
			info, err := sourceWithStatus(r.Context(), ops.Diagnostics, source)
			if err != nil {
				writeAdminError(w, err)
				return
			}
			infos = append(infos, info)
		}
		writeJSON(w, SourcesResponse{infos}, http.StatusOK)
	case Get:
		source, err := ops.Sources.GetSource(r.Context(), name)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		info, err := sourceWithStatus(r.Context(), ops.Diagnostics, source)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, info, http.StatusOK)
	case Upsert:
		var req sourceRequest
		if !decodeAdminBody(w, r, &req) {
			return
		}
		source := req.definition(name)
		if _, err := source.Kind(); err != nil {
			writeAdminError(w, err)
			return
		}
		if source.File != nil && source.File.Data != "" {
			if _, err := formats.DecodeUpstream([]byte(source.File.Data)); err != nil {
				writeAdminError(w, persistence.ErrInvalid)
				return
			}
		}
		stored, err := ops.Sources.CreateSource(r.Context(), source)
		operation, status := Operation(Create), http.StatusCreated
		// PostCommitError must precede unwrapping: even a conflict cause is committed.
		if !postCommitted(err) && errors.Is(err, persistence.ErrAlreadyExists) {
			stored, err = ops.Sources.UpdateSource(r.Context(), name, source)
			operation, status = Update, http.StatusOK
		}
		finishMutation(w, r, route, operation, sourceWire(stored), status, err)
	case Delete:
		err := ops.Sources.DeleteSource(r.Context(), name)
		finishMutation(w, r, route, Delete, nil, http.StatusNoContent, err)
	case ListEntries:
		rows, err := ops.Diagnostics.ListSourceVersions(r.Context(), name)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, SourceEntriesResponse{sourceEntriesWire(rows)}, http.StatusOK)
	}
}

// Definition/status are separate observations. Fail closed on lookup failure or
// incarnation mismatch, without a requery that could pair unrelated observations.
func sourceWithStatus(
	ctx context.Context, diagnostics AdminDiagnostics, source persistence.SourceDefinition,
) (sourceInfo, error) {
	status, err := diagnostics.Status(ctx, source.Name)
	if err != nil {
		return sourceInfo{}, err
	}
	if source.ID == "" || status.SourceID != source.ID {
		return sourceInfo{}, persistence.ErrUnavailable
	}
	info := sourceWire(source)
	info.SyncStatus = statusWire(status)
	return info, nil
}

//nolint:gocyclo,exhaustive // The closed admin inventory excludes consumer-only operations.
func serveViewAdmin(ops AdminOperations, route RouteDescriptor, name string, w http.ResponseWriter, r *http.Request) {
	switch route.Operation {
	case List:
		views, err := ops.Views.ListViews(r.Context())
		if err != nil {
			writeAdminError(w, err)
			return
		}
		infos := make([]viewInfo, 0, len(views))
		for _, view := range views {
			infos = append(infos, viewWire(view))
		}
		writeJSON(w, ViewsResponse{infos}, http.StatusOK)
	case Get:
		view, err := ops.Views.GetView(r.Context(), name)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, viewWire(view), http.StatusOK)
	case Upsert:
		var req ViewRequest
		if !decodeAdminBody(w, r, &req) {
			return
		}
		view := persistence.ViewDefinition{Name: name, Sources: req.Sources}
		if err := persistence.ValidateView(view); err != nil {
			writeAdminError(w, err)
			return
		}
		for _, source := range view.Sources {
			// Reuse the source definition's DNS name validation without IO.
			if _, err := (persistence.SourceDefinition{Name: source, Managed: &persistence.ManagedSpec{}}).Kind(); err != nil {
				writeAdminError(w, err)
				return
			}
		}
		stored, err := ops.Views.CreateView(r.Context(), view)
		operation, status := Operation(Create), http.StatusCreated
		if !postCommitted(err) && errors.Is(err, persistence.ErrAlreadyExists) {
			stored, err = ops.Views.UpdateView(r.Context(), name, view)
			operation, status = Update, http.StatusOK
		}
		finishMutation(w, r, route, operation, viewWire(stored), status, err)
	case Delete:
		err := ops.Views.DeleteView(r.Context(), name)
		finishMutation(w, r, route, Delete, nil, http.StatusNoContent, err)
	case ListEntries:
		rows, err := ops.Diagnostics.ListViewVersions(r.Context(), name)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		entries := make([]viewEntryInfo, 0, len(rows))
		for _, row := range rows {
			entries = append(entries, viewEntryInfo{EntryType: row.Kind, Name: row.Name,
				versionInfo: versionWire(row.DiagnosticVersion), SourceName: row.SourceName, Position: row.Position})
		}
		writeJSON(w, ViewEntriesResponse{entries}, http.StatusOK)
	}
}

func serveEntryAdmin(publisher EntryPublisher, route RouteDescriptor, name string, w http.ResponseWriter, r *http.Request) {
	if route.Operation == Publish {
		var req publishRequest
		if !decodeAdminBody(w, r, &req) {
			return
		}
		entry, err := req.entry()
		if err != nil {
			writeAdminError(w, err)
			return
		}
		stored, err := publisher.Publish(r.Context(), entry)
		payload := publishedWire(stored)
		if !validPayload(entry.Kind, stored) {
			payload = nil
			if err == nil {
				err = &catalog.PostCommitError{Cause: persistence.ErrUnavailable}
			}
		}
		finishMutation(w, r, route, Publish, payload, http.StatusCreated, err)
		return
	}
	kind, err := pathParam(r, route.KindParam)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	entryKind := map[string]persistence.EntryKind{
		"server": persistence.ServerKind, "skill": persistence.SkillKind, "plugin": persistence.PluginKind,
	}[kind]
	if err := entryKind.Validate(); err != nil {
		writeAdminError(w, err)
		return
	}
	if entryKind == persistence.ServerKind {
		var err error
		name, err = formats.ValidateServerName(name)
		if err != nil {
			writeError(w, "invalid server name format", http.StatusBadRequest)
			return
		}
	}
	version, err := pathParam(r, "version")
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	err = publisher.DeleteManaged(r.Context(), entryKind, name, version)
	finishMutation(w, r, route, Delete, nil, http.StatusNoContent, err)
}

func finishMutation(
	w http.ResponseWriter, r *http.Request, route RouteDescriptor, operation Operation,
	payload any, status int, err error,
) {
	if postCommitted(err) {
		observeMutation(r, route, operation)
		writeJSON(w, CommittedErrorResponse{"mutation committed; follow-up failed", true, payload}, http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		writeAdminError(w, err)
		return
	}
	observeMutation(r, route, operation)
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	writeJSON(w, payload, status)
}

//nolint:gocyclo // Ordered typed mappings retain legacy status semantics without exposing backend messages.
func writeAdminError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		writeError(w, "request canceled", http.StatusRequestTimeout)
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, "request deadline exceeded", http.StatusGatewayTimeout)
	case errors.Is(err, persistence.ErrNoManagedSource):
		writeError(w, "no managed source available", http.StatusInternalServerError)
	case errors.Is(err, persistence.ErrConfigOwned):
		writeError(w, "cannot modify config-created resource", http.StatusForbidden)
	case errors.Is(err, persistence.ErrKindChange):
		writeError(w, "source type or location change not allowed", http.StatusBadRequest)
	case errors.Is(err, persistence.ErrInvalid):
		writeError(w, "invalid request", http.StatusBadRequest)
	case errors.Is(err, persistence.ErrInUse), errors.Is(err, persistence.ErrConflict):
		writeError(w, "resource conflict", http.StatusConflict)
	case errors.Is(err, persistence.ErrReferenceNotFound):
		writeError(w, "referenced source not found", http.StatusBadRequest)
	case errors.Is(err, persistence.ErrNotFound):
		writeError(w, "not found", http.StatusNotFound)
	default:
		writeError(w, "internal server error", http.StatusInternalServerError)
	}
}
