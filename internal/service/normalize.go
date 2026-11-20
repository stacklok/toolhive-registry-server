package service

import (
	"context"
	"log/slog"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// NormalizeSourceRequest returns req with the settings v1.5.4's admin API
// accepted but the sync runtime cannot honor rewritten, logging a warning for
// each change (see normalizeSourceRequest). Call it after
// ValidateSourceConfig: settings v1.5.4 rejected stay rejected.
func NormalizeSourceRequest(ctx context.Context, name string, req *SourceCreateRequest) *SourceCreateRequest {
	out, warnings := normalizeSourceRequest(req)
	for _, warning := range warnings {
		slog.WarnContext(ctx, "Normalized source setting", "source", name, "detail", warning)
	}
	return out
}

// normalizeSourceRequest returns req with the settings v1.5.4's admin API
// accepted but the sync runtime cannot honor rewritten as config-file sources
// are (see config.NormalizeSyncInterval, config.NormalizeFileTimeout and
// config.NormalizeNameFilter), plus a warning for each change. req is not
// modified; the result shares unchanged blocks with it.
func normalizeSourceRequest(req *SourceCreateRequest) (*SourceCreateRequest, []string) {
	if req == nil {
		return nil, nil
	}
	out := *req
	var warnings []string
	if req.SyncPolicy != nil && !req.IsNonSyncedType() && !isInlineFile(req.File) {
		if interval, warning := config.NormalizeSyncInterval(req.SyncPolicy.Interval); warning != "" {
			policy := *req.SyncPolicy
			policy.Interval = interval
			out.SyncPolicy = &policy
			warnings = append(warnings, warning)
		}
	}
	if req.File != nil {
		if timeout, warning := config.NormalizeFileTimeout(req.File); warning != "" {
			file := *req.File
			file.Timeout = timeout
			out.File = &file
			warnings = append(warnings, warning)
		}
	}
	if req.Filter != nil {
		if names, warning := config.NormalizeNameFilter(req.Filter.Names); warning != "" {
			filter := *req.Filter
			filter.Names = names
			out.Filter = &filter
			warnings = append(warnings, warning)
		}
	}
	return &out, warnings
}

func isInlineFile(file *config.FileConfig) bool {
	return file != nil && file.Data != ""
}
