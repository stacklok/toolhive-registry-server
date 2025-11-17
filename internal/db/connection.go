// Package db contains code for connecting to the database.
package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // Needs to be imported for Postgres driver
	"github.com/stacklok/toolhive/pkg/logger"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
)

const (
	defaultMaxOpenConns    = 25
	defaultMaxIdleConns    = 5
	defaultConnMaxLifetime = 5 * time.Minute
	defaultSSLMode         = "require"
	defaultConnectTimeout  = 10 * time.Second
)

// Connection wraps the database connection and query interface
type Connection struct {
	DB      *sql.DB
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

	// Build connection string
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

	// Open database connection using pgx driver
	sqlDB, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	// Configure connection pool
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxIdleConns)
	sqlDB.SetConnMaxLifetime(connMaxLifetime)

	// Verify connection
	if err := sqlDB.Ping(); err != nil {
		if closeErr := sqlDB.Close(); closeErr != nil {
			logger.Errorf("Failed to close database connection after ping failure: %v", closeErr)
		}
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	logger.Infof("Database connection established: %s@%s:%d/%s",
		cfg.User, cfg.Host, cfg.Port, cfg.Database)

	// Create sqlc queries
	queries := sqlc.New(sqlDB)

	return &Connection{
		DB:      sqlDB,
		Queries: queries,
	}, nil
}

// Close closes the database connection
func (c *Connection) Close() error {
	if c.DB != nil {
		logger.Info("Closing database connection")
		return c.DB.Close()
	}
	return nil
}

// Ping verifies the database connection is still alive
func (c *Connection) Ping() error {
	if c.DB != nil {
		return c.DB.Ping()
	}
	return fmt.Errorf("database connection is nil")
}
