package database

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/trace"

	"github.com/stacklok/toolhive-registry-server/internal/otel"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// SourcePreparer completes and checks a validated source request before it is
// stored: the app scopes a Kubernetes source's namespaces (applying
// THV_REGISTRY_WATCH_NAMESPACE when none are given) and checks that the
// server can list them, as for config sources. It may modify req. An error
// rejects the request as an invalid source config.
type SourcePreparer func(ctx context.Context, name string, req *service.SourceCreateRequest) error

// WithSourcePreparer sets the hook run on every source create and update
// request after validation and before anything is written.
func WithSourcePreparer(prepare SourcePreparer) Option {
	return func(o *options) error {
		o.prepare = prepare
		return nil
	}
}

// prepareSource runs the configured SourcePreparer, if any.
func (s *dbService) prepareSource(
	ctx context.Context, span trace.Span, name string, req *service.SourceCreateRequest,
) error {
	if s.prepare == nil {
		return nil
	}
	if err := s.prepare(ctx, name, req); err != nil {
		err = fmt.Errorf("%w: %v", service.ErrInvalidSourceConfig, err)
		otel.RecordError(span, err)
		return err
	}
	return nil
}
