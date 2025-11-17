// Package db contains code for connecting to the database.
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stacklok/toolhive/pkg/logger"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
)

const (
	defaultMaxOpenConns    int32 = 25
	defaultMaxIdleConns    int32 = 5
	defaultConnMaxLifetime       = 5 * time.Minute
	defaultSSLMode               = "require"
	defaultConnectTimeout        = 10 * time.Second
)

// Connection wraps the database connection and query interface
type Connection struct {
	DB      *pgxpool.Pool
	Queries *sqlc.Queries
}

// NewConnection creates a new database connection from the provided configuration
func NewConnection(cfg *config.DatabaseConfig) (*Connection, error) {
	if cfg == nil {
		return nil, fmt.Errorf("database configuration is required")
	}

	// Validate required fields
	if cfg.Host == "" {
		return nil, fmt.Errorf("database host is required")
	}
	if cfg.Port == 0 {
		return nil, fmt.Errorf("database port is required")
	}
	if cfg.User == "" {
		return nil, fmt.Errorf("database user is required")
	}
	if cfg.Database == "" {
		return nil, fmt.Errorf("database name is required")
	}

	// Set defaults for optional fields
	sslMode := cfg.SSLMode
	if sslMode == "" {
		sslMode = defaultSSLMode
	}

	maxOpenConns := cfg.MaxOpenConns
	if maxOpenConns == 0 {
		maxOpenConns = defaultMaxOpenConns
	}

	maxIdleConns := cfg.MaxIdleConns
	if maxIdleConns == 0 {
		maxIdleConns = defaultMaxIdleConns
	}

	connMaxLifetime := defaultConnMaxLifetime
	if cfg.ConnMaxLifetime != "" {
		duration, err := time.ParseDuration(cfg.ConnMaxLifetime)
		if err != nil {
			return nil, fmt.Errorf("invalid connection max lifetime: %w", err)
		}
		connMaxLifetime = duration
	}

	// Get password using secure priority order (file -> env -> config)
	password, err := cfg.GetPassword()
	if err != nil {
		return nil, fmt.Errorf("failed to get database password: %w", err)
	}

	// Build connection string for pgx
	// Note: password is not URL-escaped here because pgx driver handles it directly
	connStr := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s connect_timeout=%d",
		cfg.Host,
		cfg.Port,
		cfg.User,
		password,
		cfg.Database,
		sslMode,
		int(defaultConnectTimeout.Seconds()),
	)

	// Parse the connection string
	poolConfig, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	// Configure connection pool
	poolConfig.MaxConns = maxOpenConns
	poolConfig.MinConns = maxIdleConns
	poolConfig.MaxConnLifetime = connMaxLifetime

	// Create connection pool
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create database connection pool: %w", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	logger.Infof("Database connection established: %s@%s:%d/%s",
		cfg.User, cfg.Host, cfg.Port, cfg.Database)

	// Create sqlc queries
	queries := sqlc.New(pool)

	return &Connection{
		DB:      pool,
		Queries: queries,
	}, nil
}

// Close closes the database connection
func (c *Connection) Close() error {
	if c.DB != nil {
		logger.Info("Closing database connection")
		c.DB.Close()
	}
	return nil
}

// Ping verifies the database connection is still alive
func (c *Connection) Ping() error {
	if c.DB != nil {
		ctx := context.Background()
		return c.DB.Ping(ctx)
	}
	return fmt.Errorf("database connection is nil")
}
