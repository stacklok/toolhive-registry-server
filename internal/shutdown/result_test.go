package shutdown_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/shutdown"
)

func TestResult(t *testing.T) {
	t.Parallel()
	backendErr := errors.New("backend failure")
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"nil", nil, nil},
		{"cancel", context.Canceled, nil},
		{"deadline", context.DeadlineExceeded, nil},
		{"wrapped cancel", fmt.Errorf("stop: %w", context.Canceled), nil},
		{"joined contexts", errors.Join(context.Canceled, context.DeadlineExceeded), nil},
		{"wrapped joined contexts", fmt.Errorf("stop: %w", errors.Join(context.Canceled, context.DeadlineExceeded)), nil},
		{"backend", backendErr, backendErr},
		{"joined backend", errors.Join(context.Canceled, backendErr), backendErr},
		{"nested backend", errors.Join(context.Canceled, errors.Join(context.DeadlineExceeded, backendErr)), backendErr},
		{"wrapped joined backend", fmt.Errorf("stop: %w", errors.Join(context.Canceled, backendErr)), backendErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := shutdown.Result(tc.err)
			if tc.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.want)
			}
		})
	}
}
