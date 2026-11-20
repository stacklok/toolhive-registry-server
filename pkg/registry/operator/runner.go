package operator

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stacklok/toolhive-registry-server/internal/shutdown"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
)

// ErrWatch means a watch could not start, closed, or returned an error event.
// The runner logs the Kubernetes reason; the host decides restart/backoff policy.
var ErrWatch = errors.New("operator watch failed")

// DefaultResyncInterval is how often an idle runner re-lists its whole scope
// when no option sets it.
const DefaultResyncInterval = 5 * time.Minute

// Runner owns only its watches and reconciliation loop, never the client or
// manager. It uses authoritative API lists, not a possibly incomplete cache.
type Runner struct {
	client      client.WithWatch
	fetcher     *Fetcher
	coordinator *reconcile.Coordinator
	retry       time.Duration
	resync      time.Duration
	running     atomic.Bool
	ready       atomic.Bool
}

// RunnerOption configures a Runner.
type RunnerOption func(*Runner) error

// WithResyncInterval sets how often a runner whose last sync succeeded re-lists
// its whole scope to catch anything its watches missed (default
// DefaultResyncInterval). Watch events sync immediately and failed syncs retry
// at PollInterval; neither waits for it. A non-positive interval is
// persistence.ErrInvalid.
func WithResyncInterval(d time.Duration) RunnerOption {
	return func(r *Runner) error {
		if d <= 0 {
			return persistence.ErrInvalid
		}
		r.resync = d
		return nil
	}
}

// NewRunner constructs a fixed-scope operator source runner. Supply a direct
// client.WithWatch (client.NewWithWatch), not a cache facade. Watch events
// trigger a sync; a failed sync retries at PollInterval, and a successful one
// is repeated at the resync interval so missed events are eventually reconciled.
func NewRunner(
	api client.WithWatch, jobs persistence.Jobs, definition persistence.SourceDefinition, options reconcile.Options,
	opts ...RunnerOption,
) (*Runner, error) {
	fetcher, err := NewFetcher(api, definition)
	if err != nil {
		return nil, err
	}
	coordinator, err := reconcile.New(jobs, fetcher, options)
	if err != nil {
		return nil, err
	}
	r := &Runner{
		client: api, fetcher: fetcher, coordinator: coordinator,
		retry: options.PollInterval, resync: DefaultResyncInterval,
	}
	for _, opt := range opts {
		if err := opt(r); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// Register adds this runner to a caller-owned manager. The host starts/stops the
// manager and configures its leader election. No controller or listener is started
// here. Manager Start propagates fatal Run errors. Register or Run, never both.
func (r *Runner) Register(mgr manager.Manager) error { return mgr.Add(r) }

// NeedLeaderElection makes a registered runner leader-only. When run directly,
// persistence leases serialize replicas; no Kubernetes leader lease is created.
func (*Runner) NeedLeaderElection() bool { return true }

// Start implements manager.Runnable and has the same lifecycle as Run.
func (r *Runner) Start(ctx context.Context) error { return r.Run(ctx) }

// Ready is false until a full startup snapshot (including empty) is committed,
// or until a sync's validation failure is recorded on the source (the source's
// previous data stays visible, as for polled sources). It stays true during
// refresh failures, and resets when Run exits. It does not assert that every
// CRD was exportable.
func (r *Runner) Ready() bool { return r.ready.Load() }

// Run starts all watches then performs a full refresh, even with zero resources.
// A validation failure (such as two exported resources with the same name and
// version) is recorded on the source, which keeps its previous data, and the
// runner keeps watching: it never ends Run. Initial access/list/commit failures,
// a changed or deleted source definition and any watch failure return to the
// host; subsequent refresh errors retry at PollInterval. Watch events that
// cannot change the export (see Fetcher.Relevant) do not trigger a refresh.
// Watches end routinely (the API server times them out), so a long-lived host
// restarts Run when it returns ErrWatch; each Run begins with a full refresh.
// Reconfigured scope/incarnation requires a new runner. Cancellation stops
// watches, cancels work, and joins it.
//
//nolint:gocyclo // One lifecycle owns watch startup, refresh, cancellation and joined cleanup.
func (r *Runner) Run(ctx context.Context) error {
	if !r.running.CompareAndSwap(false, true) {
		return persistence.ErrBusy
	}
	defer r.running.Store(false)
	defer r.ready.Store(false)
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	wake := make(chan struct{}, 1)
	fatal := make(chan error, 1)
	var workers sync.WaitGroup
	var watches []watch.Interface
	defer func() {
		cancel()
		for _, w := range watches {
			w.Stop()
		}
		workers.Wait()
	}()
	namespaces := r.fetcher.definition.Kubernetes.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{""}
	}
	for _, namespace := range namespaces {
		for _, list := range resourceLists() {
			w, err := r.client.Watch(work, list, client.InNamespace(namespace))
			if err != nil {
				if ctx.Err() != nil && shutdown.Result(err) == nil {
					return nil
				}
				slog.WarnContext(ctx, "Failed to start Kubernetes watch", "source", r.fetcher.definition.Name,
					"kind", listKind(list), "namespace", namespace, "error", ingest.RedactURLs(err.Error()))
				return ErrWatch
			}
			watches = append(watches, w)
			workers.Add(1)
			p := pump{source: r.fetcher.definition.Name, kind: listKind(list), namespace: namespace, relevant: r.fetcher.Relevant}
			go func() { defer workers.Done(); p.run(work, cancel, w, wake, fatal) }()
		}
	}
	timer := time.NewTimer(r.resync)
	defer timer.Stop()
	for {
		err := r.coordinator.Sync(work, r.fetcher.definition.Name)
		select {
		case failure := <-fatal:
			return errors.Join(failure, shutdown.Result(err))
		default:
		}
		if ctx.Err() != nil {
			return shutdown.Result(err)
		}
		var recorded *reconcile.FailureError
		switch {
		case err == nil:
			r.ready.Store(true)
		case errors.As(err, &recorded) && recorded.Category == persistence.FailureValidation:
			// Recorded on the source with its previous data kept; a later event
			// or retry may fix it. Like reconcile.Coordinator, keep running.
			r.ready.Store(true)
		case errors.Is(err, errDefinitionChanged) || errors.Is(err, persistence.ErrInvalid) ||
			errors.Is(err, persistence.ErrNotFound) || (!r.ready.Load() && !errors.Is(err, persistence.ErrBusy)):
			return err
		}
		// An idle runner relies on its watches; only a failed sync retries soon.
		next := r.resync
		if err != nil {
			next = r.retry
		}
		timer.Reset(next)
		select {
		case <-ctx.Done():
			return nil
		case err := <-fatal:
			return err
		case <-wake:
		case <-timer.C:
		}
	}
}

// pump forwards relevant events of one watch to the refresh loop.
type pump struct {
	source, kind, namespace string
	relevant                func(watch.Event) bool
}

func (p pump) run(ctx context.Context, cancel context.CancelFunc, w watch.Interface, wake chan<- struct{}, fatal chan<- error) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-w.ResultChan():
			if !ok || event.Type == watch.Error {
				if ctx.Err() == nil {
					p.logEnd(ctx, event, ok)
					select {
					case fatal <- ErrWatch:
					default:
					}
					cancel()
				}
				return
			}
			if event.Type != watch.Bookmark && p.relevant(event) {
				select {
				case wake <- struct{}{}:
				default:
				}
			}
		}
	}
}

func (p pump) logEnd(ctx context.Context, event watch.Event, open bool) {
	attrs := []any{"source", p.source, "kind", p.kind, "namespace", p.namespace}
	if !open {
		slog.DebugContext(ctx, "Kubernetes watch ended", attrs...)
		return
	}
	if status, ok := event.Object.(*metav1.Status); ok {
		attrs = append(attrs, "reason", string(status.Reason), "code", status.Code, "error", ingest.RedactURLs(status.Message))
	}
	slog.WarnContext(ctx, "Kubernetes watch failed", attrs...)
}
