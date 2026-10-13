package operator

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stacklok/toolhive-registry-server/internal/shutdown"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
)

// ErrWatch means a watch could not start, closed, or returned an error event.
// Details are deliberately not logged; the host decides restart/backoff policy.
var ErrWatch = errors.New("operator watch failed")

// Runner owns only its watches and reconciliation loop, never the client or
// manager. It uses authoritative API lists, not a possibly incomplete cache.
type Runner struct {
	client      client.WithWatch
	fetcher     *Fetcher
	coordinator *reconcile.Coordinator
	interval    time.Duration
	running     atomic.Bool
	ready       atomic.Bool
}

// NewRunner constructs a fixed-scope operator source runner. Supply a direct
// client.WithWatch (client.NewWithWatch), not a cache facade. PollInterval is also
// the bounded retry/resync interval, so missed events are eventually reconciled.
func NewRunner(
	api client.WithWatch, jobs persistence.Jobs, definition persistence.SourceDefinition, options reconcile.Options,
) (*Runner, error) {
	fetcher, err := NewFetcher(api, definition)
	if err != nil {
		return nil, err
	}
	coordinator, err := reconcile.New(jobs, fetcher, options)
	if err != nil {
		return nil, err
	}
	return &Runner{client: api, fetcher: fetcher, coordinator: coordinator, interval: options.PollInterval}, nil
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

// Ready is false until a full startup snapshot (including empty) is committed.
// It stays true during transient refresh failures with the last good snapshot,
// and resets when Run exits. It does not assert that every CRD was exportable.
func (r *Runner) Ready() bool { return r.ready.Load() }

// Run starts all watches then performs a full refresh, even with zero resources.
// Initial access/list/commit failures and any watch failure return to the host;
// subsequent refresh errors retry at PollInterval. Reconfigured scope/incarnation
// requires a new runner. Cancellation stops watches, cancels work, and joins it.
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
				return ErrWatch
			}
			watches = append(watches, w)
			workers.Add(1)
			go func() { defer workers.Done(); pump(work, cancel, w, wake, fatal) }()
		}
	}
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
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
		if err == nil {
			r.ready.Store(true)
		} else if errors.Is(err, persistence.ErrInvalid) || errors.Is(err, persistence.ErrNotFound) ||
			(!r.ready.Load() && !errors.Is(err, persistence.ErrBusy)) {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case err := <-fatal:
			return err
		case <-wake:
		case <-ticker.C:
		}
	}
}

func pump(ctx context.Context, cancel context.CancelFunc, w watch.Interface, wake chan<- struct{}, fatal chan<- error) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-w.ResultChan():
			if !ok || event.Type == watch.Error {
				if ctx.Err() == nil {
					select {
					case fatal <- ErrWatch:
					default:
					}
					cancel()
				}
				return
			}
			if event.Type != watch.Bookmark {
				select {
				case wake <- struct{}{}:
				default:
				}
			}
		}
	}
}
