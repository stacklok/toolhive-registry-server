// Package standalone supervises leased ingestion for CatalogApp and the CLI.
// Never co-own its sources with legacy assembly.
package standalone

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stacklok/toolhive-registry-server/internal/shutdown"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
)

// Sources discovers all persisted definitions, independent of view links/origin.
//
//go:generate mockgen -destination=mocks/sources.go -package=mocks . Sources
type Sources interface {
	ListSources(context.Context) ([]persistence.SourceDefinition, error)
}

//go:generate mockgen -destination=mocks/worker.go -package=mocks . Worker

// Worker borrows its dependencies and joins its own work before Run returns.
type Worker interface {
	Run(context.Context) error
	Ready() bool
}

// Options supplies a dormant operator factory and read-only compatibility check.
// Callbacks and injected I/O must honor cancellation. No implicit scope fallback.
type Options struct {
	Reconcile reconcile.Options
	Operator  func(persistence.SourceDefinition) (Worker, error)
	Preflight func(context.Context, persistence.SourceDefinition) error
}

type owned struct {
	definition persistence.SourceDefinition
	worker     Worker
	cancel     context.CancelFunc
	done       chan error
}

type refreshRequest struct {
	ctx    context.Context
	result chan error
}

// Runtime owns only its workers, not the supplied sources, Jobs, or clients.
type Runtime struct {
	sources     Sources
	jobs        persistence.Jobs
	fetcher     reconcile.Fetcher
	coordinator *reconcile.Coordinator
	options     Options
	running     bool
	ready       atomic.Bool
	mu          sync.Mutex
	workers     map[string]*owned
	refresh     chan refreshRequest
	done        chan struct{}
	wake        chan struct{}
}

// New validates only; it performs no list, fetch, lease, watch or database work.
func New(sources Sources, jobs persistence.Jobs, fetcher reconcile.Fetcher, options Options) (*Runtime, error) {
	if sources == nil || options.Operator == nil || options.Preflight == nil {
		return nil, persistence.ErrInvalid
	}
	coordinator, err := reconcile.New(jobs, fetcher, options.Reconcile)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		sources: sources, jobs: jobs, fetcher: fetcher, coordinator: coordinator, options: options,
		workers: make(map[string]*owned),
	}, nil
}

// Preflight reads the complete persisted set without starting any worker.
func (r *Runtime) Preflight(ctx context.Context) error {
	_, err := r.definitions(ctx, true)
	return err
}

// Ready requires discovery, scheduled polling and every current worker's startup.
func (r *Runtime) Ready() bool {
	if !r.ready.Load() || !r.coordinator.Ready() {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, w := range r.workers {
		if !w.worker.Ready() {
			return false
		}
	}
	return r.ready.Load()
}

// Refresh serializes ownership changes with Run. Failed listing/validation never
// becomes an empty desired set. Old incarnations/scopes are canceled and joined first.
func (r *Runtime) Refresh(ctx context.Context) error {
	r.mu.Lock()
	running, refresh, done := r.running, r.refresh, r.done
	r.mu.Unlock()
	if !running {
		return persistence.ErrBusy
	}
	req := refreshRequest{ctx: ctx, result: make(chan error, 1)}
	select {
	case refresh <- req:
	case <-done:
		return persistence.ErrBusy
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-req.result:
		return err
	case <-done:
		return persistence.ErrBusy
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SyncInline leases the current persisted definition, never caller-supplied data.
// The leased definition is checked before the supplied fetcher can read it.
// Calls are caller-owned and independent of Run. The host must gate new calls
// and cancel/drain in-flight calls before closing borrowed storage; Run only
// joins its background workers.
func (r *Runtime) SyncInline(ctx context.Context, name string) error {
	c, err := reconcile.New(r.jobs, inlineFetcher{fetcher: r.fetcher}, r.options.Reconcile)
	if err != nil {
		return err
	}
	return c.Sync(ctx, name)
}

// Run blocks until cancellation or a terminal worker/backend failure. All owned
// siblings are canceled and joined on every exit; borrowed resources stay open.
//
//nolint:gocyclo // Keep initiating-failure classification and sibling cleanup on the owning lifecycle goroutine.
func (r *Runtime) Run(ctx context.Context) (err error) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return persistence.ErrBusy
	}
	r.done = make(chan struct{})
	r.refresh = make(chan refreshRequest)
	r.wake = make(chan struct{}, 1)
	r.running = true
	r.mu.Unlock()
	defer func() { r.mu.Lock(); r.running = false; close(r.done); r.mu.Unlock() }()
	work, cancel := context.WithCancel(ctx)
	var pollDone chan error
	defer func() {
		r.ready.Store(false)
		// Classify the initiating result before canceling our own lifetime.
		if ctx.Err() != nil {
			err = shutdown.Result(err)
		} else if err != nil && shutdown.Result(err) == nil {
			err = errors.Join(persistence.ErrUnavailable, err)
		}
		cancel()
		if pollDone != nil {
			err = errors.Join(err, shutdown.Result(<-pollDone))
		}
		err = errors.Join(err, r.stopAll())
	}()
	if err := r.replace(work, work, true, nil, cancel); err != nil {
		return err
	}
	pollDone = make(chan error, 1)
	go func() { pollDone <- r.coordinator.Run(work) }()
	ticker := time.NewTicker(r.options.Reconcile.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-pollDone:
			pollDone <- err
			return err
		case <-r.wake:
		case req := <-r.refresh:
			requestCtx, requestCancel := context.WithCancel(req.ctx)
			stop := context.AfterFunc(work, requestCancel)
			err := r.replace(requestCtx, work, false, pollDone, cancel)
			stop()
			requestCancel()
			req.result <- err
			if work.Err() != nil || (err != nil && !r.ready.Load()) {
				return err
			}
		case <-ticker.C:
			if err := r.replace(work, work, false, pollDone, cancel); err != nil {
				return err
			}
		}
		if err := r.checkWorkers(work, pollDone, cancel); err != nil {
			return err
		}
	}
}

func (r *Runtime) definitions(ctx context.Context, preflight bool) ([]persistence.SourceDefinition, error) {
	definitions, err := r.sources.ListSources(ctx)
	if err != nil {
		return nil, err
	}
	for _, d := range definitions {
		if _, err := d.Kind(); err != nil {
			return nil, err
		}
		if d.ID == "" {
			return nil, persistence.ErrInvalid
		}
		if preflight {
			if err := r.options.Preflight(ctx, d); err != nil {
				return nil, err
			}
		}
	}
	return definitions, nil
}

// Only discovery runs concurrently; ownership changes stay on Run's goroutine.
// A terminal event cancels the whole run before joining the pending discovery.
func (r *Runtime) discover(
	ctx, lifetime context.Context, preflight bool, pollDone chan error, cancel context.CancelFunc,
) (_ []persistence.SourceDefinition, result error) {
	ctx, discoveryCancel := context.WithCancel(ctx)
	done := make(chan struct{})
	var definitions []persistence.SourceDefinition
	var err error
	go func() {
		defer close(done)
		definitions, err = r.definitions(ctx, preflight)
	}()
	defer func() {
		discoveryCancel()
		<-done
		result = errors.Join(result, err)
	}()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case failure := <-pollDone:
			pollDone <- failure
			cancel()
			return nil, failure
		case <-r.wake:
			if failure := r.workerError(lifetime); failure != nil && !ownershipError(failure) {
				cancel()
				return nil, failure
			}
		case <-done:
			if failure := r.workerError(lifetime); failure != nil && !ownershipError(failure) {
				cancel()
				return nil, failure
			}
			return definitions, nil
		}
	}
}

func (r *Runtime) replace(
	ctx, lifetime context.Context, preflight bool, pollDone chan error, cancel context.CancelFunc,
) error {
	definitions, err := r.discover(ctx, lifetime, preflight, pollDone, cancel)
	if err != nil {
		return err
	}
	r.ready.Store(false)
	desired := make(map[string]persistence.SourceDefinition)
	for _, d := range definitions {
		if d.Kubernetes != nil || (d.File != nil && d.File.Data != "") {
			desired[d.Name] = d
		}
	}
	for name, old := range r.workers {
		if sameOwner(old.definition, desired[name]) {
			delete(desired, name)
			continue
		}
		r.mu.Lock()
		delete(r.workers, name)
		r.mu.Unlock()
		old.cancel()
		if err := shutdown.Result(<-old.done); err != nil && !ownershipError(err) {
			return err
		}
	}
	for name, d := range desired {
		var w Worker
		if d.Kubernetes != nil {
			w, err = r.options.Operator(d)
		} else {
			w = &inlineWorker{jobs: r.jobs, definition: d, fetcher: r.fetcher, options: r.options.Reconcile}
		}
		if err != nil {
			return err
		}
		work, cancel := context.WithCancel(lifetime)
		owned := &owned{definition: d, worker: w, cancel: cancel, done: make(chan error, 1)}
		r.mu.Lock()
		r.workers[name] = owned
		r.mu.Unlock()
		wake := r.wake
		go func() {
			owned.done <- w.Run(work)
			if work.Err() != nil {
				return
			}
			select {
			case wake <- struct{}{}:
			default:
			}
		}()
	}
	r.ready.Store(true)
	return nil
}

func ownershipError(err error) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		matched := false
		for _, child := range joined.Unwrap() {
			if shutdown.Result(child) == nil {
				continue
			}
			if !ownershipError(child) {
				return false
			}
			matched = true
		}
		return matched
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok && wrapped.Unwrap() != nil {
		return ownershipError(wrapped.Unwrap())
	}
	return errors.Is(err, persistence.ErrInvalid) || errors.Is(err, persistence.ErrStale) || errors.Is(err, persistence.ErrNotFound)
}

func sameOwner(a, b persistence.SourceDefinition) bool {
	if a.ID != b.ID || (a.Kubernetes == nil) != (b.Kubernetes == nil) {
		return false
	}
	if a.Kubernetes == nil {
		return true
	}
	x, y := slices.Clone(a.Kubernetes.Namespaces), slices.Clone(b.Kubernetes.Namespaces)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}

func (r *Runtime) checkWorkers(ctx context.Context, pollDone chan error, cancel context.CancelFunc) error {
	err := r.workerError(ctx)
	// A public runner can observe a changed fence before discovery does.
	// Re-read ownership once before treating that mismatch as terminal.
	if ownershipError(err) {
		if err = r.replace(ctx, ctx, false, pollDone, cancel); err == nil {
			err = r.workerError(ctx)
		}
	}
	return err
}

func (r *Runtime) workerError(lifetime context.Context) error {
	var mismatch error
	for _, w := range r.workers {
		select {
		case err := <-w.done:
			if err == nil {
				if lifetime.Err() != nil {
					w.done <- nil
					continue
				}
				err = persistence.ErrUnavailable
			}
			w.done <- err
			if !ownershipError(err) {
				return err
			}
			mismatch = err
		default:
		}
	}
	return mismatch
}

func (r *Runtime) stopAll() error {
	r.mu.Lock()
	workers := r.workers
	r.workers = make(map[string]*owned)
	r.mu.Unlock()
	for _, w := range workers {
		w.cancel()
	}
	var err error
	for _, w := range workers {
		err = errors.Join(err, shutdown.Result(<-w.done))
	}
	return err
}

type inlineFetcher struct {
	fetcher reconcile.Fetcher
	id      string
}

func (f inlineFetcher) Fetch(ctx context.Context, d persistence.SourceDefinition) (ingest.Result, error) {
	if _, err := d.Kind(); err != nil {
		return ingest.Result{}, err
	}
	if d.File == nil || d.File.Data == "" {
		return ingest.Result{}, persistence.ErrInvalid
	}
	if f.id != "" && f.id != d.ID {
		return ingest.Result{}, persistence.ErrStale
	}
	return f.fetcher.Fetch(ctx, d)
}

type inlineWorker struct {
	jobs       persistence.Jobs
	definition persistence.SourceDefinition
	fetcher    reconcile.Fetcher
	options    reconcile.Options
	ready      atomic.Bool
}

func retryInline(err error) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if !retryInline(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok && wrapped.Unwrap() != nil {
		return retryInline(wrapped.Unwrap())
	}
	return err == nil || ownershipError(err) || errors.Is(err, persistence.ErrBusy) || errors.Is(err, ingest.ErrFetch)
}

func (w *inlineWorker) Ready() bool { return w.ready.Load() }
func (w *inlineWorker) Run(ctx context.Context) error {
	defer w.ready.Store(false)
	c, err := reconcile.New(w.jobs, inlineFetcher{fetcher: w.fetcher, id: w.definition.ID}, w.options)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(w.options.PollInterval)
	defer ticker.Stop()
	for {
		status, err := w.jobs.Status(ctx, w.definition.Name)
		w.ready.Store(err == nil && status.SourceID == w.definition.ID && status.BaselineValid)
		if err == nil && status.SourceID == w.definition.ID {
			if !status.BaselineValid {
				err = c.Sync(ctx, w.definition.Name)
			}
		}
		if ctx.Err() != nil {
			return shutdown.Result(err)
		}
		if !retryInline(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
