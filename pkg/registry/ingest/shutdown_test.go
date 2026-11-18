package ingest_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

func TestFetchMixedCancellationRemainsSanitizedFailure(t *testing.T) {
	t.Parallel()
	for _, canceled := range []bool{false, true} {
		for _, mixed := range []bool{false, true} {
			t.Run(fmt.Sprintf("canceled=%t/mixed=%t", canceled, mixed), func(t *testing.T) {
				t.Parallel()
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				fetcher := ingest.New(ingest.Options{ReadFile: func(ctx context.Context, _ string) ([]byte, error) {
					if canceled {
						cancel()
						<-ctx.Done()
					}
					err := errors.Join(context.Canceled, context.DeadlineExceeded)
					if mixed {
						err = errors.Join(err, errors.New("SECRET transport detail"))
					}
					return nil, fmt.Errorf("SECRET wrapper: %w", err)
				}})
				result, err := fetcher.Fetch(ctx, persistence.SourceDefinition{Name: "file", File: &persistence.FileSpec{Path: "/fixture/registry.json"}, Schedule: "1h"})
				require.ErrorIs(t, err, context.Canceled)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				if mixed {
					require.ErrorIs(t, err, ingest.ErrFetch)
				} else {
					require.NotErrorIs(t, err, ingest.ErrFetch)
				}
				require.NotContains(t, err.Error(), "SECRET")
				require.Empty(t, result.Hash)
				require.Empty(t, result.Snapshot.Data.Servers)
			})
		}
	}
}
