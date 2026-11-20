package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/sync/standalone"
	standalonemocks "github.com/stacklok/toolhive-registry-server/internal/sync/standalone/mocks"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
	reconcilemocks "github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

// lockedBuffer is a bytes.Buffer safe for concurrent writes.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// The restart delay is logged as a duration string, not integer nanoseconds.
//
//nolint:paralleltest // replaces the default logger
func TestSyncRunner_LogsRetryDelayAsDuration(t *testing.T) {
	logs := &lockedBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	ctrl := gomock.NewController(t)
	sources := standalonemocks.NewMockSources(ctrl)
	sources.EXPECT().ListSources(gomock.Any()).Return(nil, errors.New("list failed")).AnyTimes()
	runtime, err := standalone.New(sources, reconcilemocks.NewMockJobs(ctrl), reconcilemocks.NewMockFetcher(ctrl), standalone.Options{
		Reconcile: reconcile.Options{PollInterval: time.Hour, LeaseDuration: time.Minute},
		Operator:  func(persistence.SourceDefinition) (standalone.Worker, error) { return nil, persistence.ErrInvalid },
		Preflight: func(context.Context, persistence.SourceDefinition) error { return nil },
	})
	require.NoError(t, err)
	runner := &syncRunner{runtime: runtime, restartMin: 1500 * time.Millisecond, restartMax: time.Hour}
	done := make(chan error, 1)
	go func() { done <- runner.Start(context.Background()) }()
	require.Eventually(t, func() bool {
		return bytes.Contains([]byte(logs.String()), []byte(`"retry_in":"1.5s"`))
	}, 5*time.Second, 5*time.Millisecond, "retry_in is a duration string: %s", logs.String())
	require.NoError(t, runner.Stop())
	require.NoError(t, <-done)
}
