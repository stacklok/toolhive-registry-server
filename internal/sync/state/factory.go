package state

import (
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewStateService creates a database-backed RegistryStateService.
// The pool parameter must not be nil.
func NewStateService(pool *pgxpool.Pool) (RegistryStateService, error) {
	if pool == nil {
		return nil, fmt.Errorf("database pool is required")
	}
	return NewDBStateService(pool), nil
}
