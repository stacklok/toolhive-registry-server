package kubernetes

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/operator"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// Restart delays. The API server ends every watch after its request timeout
// (30-60 minutes by default), so a restart after a long run is quick; a source
// that keeps failing backs off up to watchRestartMax.
var (
	watchRestartMin   = time.Second
	watchRestartMax   = 30 * time.Second
	watchRestartReset = time.Minute
)

// OperatorWorker runs a Kubernetes source's runner for the life of the server.
// The library leaves restart policy to the host: a runner returns
// operator.ErrWatch whenever one of its watches ends, and returns its first
// failure before it has synced. This worker runs it again in both cases once
// the source has synced at least once, so neither a routine watch timeout nor
// a transient failure right after one stops sync. Each run re-establishes the
// watches and starts with a full refresh, so nothing changed meanwhile is
// missed. Ownership errors (the definition changed or was removed) still
// return, for the runtime to replace the worker.
type OperatorWorker struct {
	runner *operator.Runner
	source string
	// synced is set once any sync of this source completed or committed.
	synced atomic.Bool
}

// Ready reports whether the runner has committed its startup snapshot. It is
// false while a restart is pending.
func (w *OperatorWorker) Ready() bool { return w.runner.Ready() }

// Run runs the runner until ctx ends or it fails in a way a restart cannot fix.
func (w *OperatorWorker) Run(ctx context.Context) error {
	delay := watchRestartMin
	for {
		started := time.Now()
		err := w.runner.Run(ctx)
		if ctx.Err() != nil || !w.restartable(err) {
			return err
		}
		routine := time.Since(started) >= watchRestartReset
		if routine {
			delay = watchRestartMin
		}
		w.logRestart(ctx, err, routine, delay)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		delay = min(delay*2, watchRestartMax)
	}
}

func (w *OperatorWorker) restartable(err error) bool {
	if err == nil || errors.Is(err, persistence.ErrInvalid) || errors.Is(err, persistence.ErrNotFound) ||
		errors.Is(err, persistence.ErrStale) {
		return false
	}
	return errors.Is(err, operator.ErrWatch) || w.synced.Load()
}

// logRestart reports a restart. Runner errors are sentinels without upstream
// detail, so they are safe to log.
func (w *OperatorWorker) logRestart(ctx context.Context, err error, routine bool, delay time.Duration) {
	switch {
	case routine && errors.Is(err, operator.ErrWatch):
		slog.InfoContext(ctx, "Kubernetes watch ended, restarting source sync",
			"source", w.source, "retry_in", delay.String())
	case !w.synced.Load():
		slog.WarnContext(ctx, "Kubernetes source sync could not start; check that the server can list and watch the "+
			"toolhive resources in the configured namespaces", "source", w.source, "error", err, "retry_in", delay.String())
	default:
		slog.WarnContext(ctx, "Kubernetes source sync stopped, restarting",
			"source", w.source, "error", err, "retry_in", delay.String())
	}
}
