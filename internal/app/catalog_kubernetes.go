package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/stacklok/toolhive-registry-server/internal/app/storage"
	"github.com/stacklok/toolhive-registry-server/internal/kubernetes"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	database "github.com/stacklok/toolhive-registry-server/internal/service/db"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// startupPreflight checks a config source before it is stored. Invalid
// configuration stops startup. A Kubernetes source whose resources cannot be
// listed (no permission, CRDs missing, API server unreachable) does not: as
// in v1.5.4 the failure is logged with its cause, the other sources are
// served, and the source's worker keeps retrying.
// logKeyError is the structured-log key for an error value.
const logKeyError = "error"

func startupPreflight(
	ctx context.Context, d persistence.SourceDefinition, preflight func(context.Context, persistence.SourceDefinition) error,
) error {
	err := preflight(ctx, d)
	if err == nil || d.Kubernetes == nil || ctx.Err() != nil || errors.Is(err, persistence.ErrInvalid) {
		return err
	}
	attrs := []any{"source", d.Name, "namespaces", d.Kubernetes.Namespaces, logKeyError, err}
	var listErr *kubernetes.PreflightError
	if errors.As(err, &listErr) {
		attrs = []any{"source", d.Name, "kind", listErr.Kind, "namespace", listErr.Namespace,
			"reason", listErr.Reason(), logKeyError, listErr.Cause()}
	}
	slog.ErrorContext(ctx, "Kubernetes source cannot be synced yet; serving other sources and retrying", attrs...)
	return nil
}

// kubernetesSourcePreparer gives Kubernetes sources created or updated through
// the admin API the same namespace scope and preflight as config sources: no
// namespaces means the watch namespaces (THV_REGISTRY_WATCH_NAMESPACE), and
// the server must be able to list the scope.
func kubernetesSourcePreparer(
	watch string, preflight func(context.Context, persistence.SourceDefinition) error,
) database.SourcePreparer {
	return func(ctx context.Context, name string, req *service.SourceCreateRequest) error {
		if req.Kubernetes == nil {
			return nil
		}
		namespaces, err := storage.EffectiveNamespaces(req.Kubernetes.Namespaces, watch)
		if err != nil {
			return err
		}
		scoped := *req.Kubernetes
		scoped.Namespaces = namespaces
		req.Kubernetes = &scoped
		return preflight(ctx, persistence.SourceDefinition{
			Name: name, Kubernetes: &persistence.KubernetesSpec{Namespaces: namespaces},
		})
	}
}
