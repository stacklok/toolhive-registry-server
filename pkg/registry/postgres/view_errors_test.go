package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

func TestViewReadErrorCategories(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name               string
		err, errorCategory error
	}{
		{"driver", errors.New("private driver detail"), persistence.ErrUnavailable},
		{"serialization", &pgconn.PgError{Code: "40001", Message: "private SQL detail"}, persistence.ErrUnavailable},
		{"deadlock", &pgconn.PgError{Code: "40P01", Message: "private SQL detail"}, persistence.ErrUnavailable},
		{"sql", &pgconn.PgError{Code: "42P01", Message: "private SQL detail"}, persistence.ErrUnavailable},
		{"cancel", fmt.Errorf("wrapped: %w", context.Canceled), context.Canceled},
		{"deadline", fmt.Errorf("wrapped: %w", context.DeadlineExceeded), context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := viewReadError(tc.err)
			if !errors.Is(got, tc.errorCategory) || got.Error() != tc.errorCategory.Error() {
				t.Fatalf("error leaked or lost category: %v", got)
			}
		})
	}
}
