// Package database provides a database-backed implementation of the RegistryService interface
package database

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

const (
	// unknownSubtype is used when a file source subtype cannot be determined
	unknownSubtype = "unknown"
)

var (
	// ErrBug is returned when a server is not found
	ErrBug = errors.New("bug")
)

// serverCursor represents a cursor for pagination based on server name and version.
// This provides deterministic ordering regardless of timestamp values.
type serverCursor struct {
	Name    string
	Version string
}

// options holds configuration options for the database service
type options struct {
	pool        *pgxpool.Pool
	tracer      trace.Tracer
	maxMetaSize int
}

// Option is a functional option for configuring the database service
type Option func(*options) error

// WithConnectionPool creates a new database-backed registry service with the
// given pgx pool. The caller is responsible for closing the pool when it is
// done.
func WithConnectionPool(pool *pgxpool.Pool) Option {
	return func(o *options) error {
		if pool == nil {
			return fmt.Errorf("pgx pool is required")
		}
		o.pool = pool
		return nil
	}
}

// WithTracer sets the OpenTelemetry tracer for the database service.
// If not set, tracing will be disabled (no-op).
func WithTracer(tracer trace.Tracer) Option {
	return func(o *options) error {
		o.tracer = tracer
		return nil
	}
}

// WithMaxMetaSize sets the maximum allowed size in bytes for publisher-provided
// metadata extensions. The value must be greater than zero.
func WithMaxMetaSize(maxMetaSize int) Option {
	return func(o *options) error {
		if maxMetaSize <= 0 {
			return fmt.Errorf("maxMetaSize must be greater than zero, got %d", maxMetaSize)
		}
		o.maxMetaSize = maxMetaSize
		return nil
	}
}

// dbService implements the RegistryService interface using a database backend
type dbService struct {
	pool        *pgxpool.Pool
	tracer      trace.Tracer
	maxMetaSize int
}

var _ service.RegistryService = (*dbService)(nil)

// New creates a new database-backed registry service with the given options
func New(opts ...Option) (service.RegistryService, error) {
	o := &options{}

	for _, opt := range opts {
		if err := opt(o); err != nil {
			return nil, err
		}
	}

	return &dbService{
		pool:        o.pool,
		tracer:      o.tracer,
		maxMetaSize: o.maxMetaSize,
	}, nil
}

// deserializeSourceConfig deserializes source config from JSON bytes based on source type
func deserializeSourceConfig(sourceType string, data []byte) any {
	if len(data) == 0 {
		return nil
	}

	switch config.SourceType(sourceType) {
	case config.SourceTypeGit:
		var cfg config.GitConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil
		}
		return &cfg
	case config.SourceTypeAPI:
		var cfg config.APIConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil
		}
		return &cfg
	case config.SourceTypeFile:
		var cfg config.FileConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil
		}
		return &cfg
	case config.SourceTypeManaged:
		var cfg config.ManagedConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil
		}
		return &cfg
	case config.SourceTypeKubernetes:
		var cfg config.KubernetesConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil
		}
		return &cfg
	default:
		return nil
	}
}

// deserializeFilterConfig deserializes filter config from JSON bytes
func deserializeFilterConfig(data []byte) *config.FilterConfig {
	if len(data) == 0 {
		return nil
	}

	var cfg config.FilterConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return &cfg
}
