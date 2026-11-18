package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/telemetry"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	reconcilemocks "github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

const (
	syncDurationMetric = "stacklok.registry.sync.duration"
	errorsMetric       = "stacklok.registry.errors"
)

// recordedSync is one stacklok.registry.sync.duration data point.
type recordedSync struct {
	source  string
	outcome string
	count   uint64
}

// metricsHarness wraps a MockJobs in measuredJobs with an in-memory reader.
type metricsHarness struct {
	jobs   *reconcilemocks.MockJobs
	sut    *measuredJobs
	reader *sdkmetric.ManualReader
}

func newMetricsHarness(t *testing.T) metricsHarness {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	syncMetrics, err := telemetry.NewSyncMetrics(provider)
	require.NoError(t, err)
	jobs := reconcilemocks.NewMockJobs(gomock.NewController(t))
	return metricsHarness{jobs: jobs, sut: newMeasuredJobs(jobs, syncMetrics), reader: reader}
}

func (h metricsHarness) collect(t *testing.T) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, h.reader.Collect(context.Background(), &rm))
	return rm
}

func findMetric(rm metricdata.ResourceMetrics, name string) (metricdata.Metrics, bool) {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return m, true
			}
		}
	}
	return metricdata.Metrics{}, false
}

func attr(set attribute.Set, key string) string {
	v, _ := set.Value(attribute.Key(key))
	return v.AsString()
}

func (h metricsHarness) syncs(t *testing.T) []recordedSync {
	t.Helper()
	m, ok := findMetric(h.collect(t), syncDurationMetric)
	if !ok {
		return nil
	}
	hist, ok := m.Data.(metricdata.Histogram[float64])
	require.True(t, ok, "sync duration must be a float64 histogram")
	out := make([]recordedSync, 0, len(hist.DataPoints))
	for _, dp := range hist.DataPoints {
		out = append(out, recordedSync{source: attr(dp.Attributes, "source"), outcome: attr(dp.Attributes, "outcome"), count: dp.Count})
	}
	return out
}

func (h metricsHarness) syncErrors(t *testing.T) map[string]int64 {
	t.Helper()
	m, ok := findMetric(h.collect(t), errorsMetric)
	if !ok {
		return nil
	}
	sum, ok := m.Data.(metricdata.Sum[int64])
	require.True(t, ok, "errors must be an int64 sum")
	out := map[string]int64{}
	for _, dp := range sum.DataPoints {
		assert.Equal(t, "sync", attr(dp.Attributes, "area"))
		out[attr(dp.Attributes, "error_type")] += dp.Value
	}
	return out
}

func testJob(lease, source string) persistence.Job {
	return persistence.Job{
		Source:     persistence.SourceDefinition{ID: "id-" + source, Name: source},
		LeaseID:    lease,
		Generation: 3,
		ExpiresAt:  time.Unix(1700000000, 0),
	}
}

func TestMeasuredJobs_RecordsCompleteAttempts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	result := persistence.SyncResult{Hash: "h", FilterHash: "f"}
	snapshot := model.Snapshot{}

	tests := []struct {
		name       string
		run        func(t *testing.T, h metricsHarness)
		wantSyncs  []recordedSync
		wantErrors map[string]int64
	}{
		{
			name: "ClaimNext then Complete records success",
			run: func(t *testing.T, h metricsHarness) {
				t.Helper()
				job := testJob("l1", "git-src")
				h.jobs.EXPECT().ClaimNext(ctx, time.Minute).Return(job, true, nil)
				h.jobs.EXPECT().Complete(ctx, job, result).Return(nil)
				got, ok, err := h.sut.ClaimNext(ctx, time.Minute)
				require.NoError(t, err)
				require.True(t, ok)
				assert.Equal(t, job, got)
				require.NoError(t, h.sut.Complete(ctx, job, result))
			},
			wantSyncs: []recordedSync{{source: "git-src", outcome: "success", count: 1}},
		},
		{
			name: "Acquire then CommitSnapshot records success",
			run: func(t *testing.T, h metricsHarness) {
				t.Helper()
				job := testJob("l2", "inline")
				h.jobs.EXPECT().Acquire(ctx, "inline", time.Minute).Return(job, nil)
				h.jobs.EXPECT().CommitSnapshot(ctx, job, result, snapshot).Return(nil)
				got, err := h.sut.Acquire(ctx, "inline", time.Minute)
				require.NoError(t, err)
				assert.Equal(t, job, got)
				require.NoError(t, h.sut.CommitSnapshot(ctx, job, result, snapshot))
			},
			wantSyncs: []recordedSync{{source: "inline", outcome: "success", count: 1}},
		},
		{
			name: "Fail records error outcome and error type",
			run: func(t *testing.T, h metricsHarness) {
				t.Helper()
				job := testJob("l3", "api-src")
				h.jobs.EXPECT().ClaimNext(ctx, time.Minute).Return(job, true, nil)
				h.jobs.EXPECT().Fail(ctx, job, persistence.FailureFetch).Return(nil)
				_, _, err := h.sut.ClaimNext(ctx, time.Minute)
				require.NoError(t, err)
				require.NoError(t, h.sut.Fail(ctx, job, persistence.FailureFetch))
			},
			wantSyncs:  []recordedSync{{source: "api-src", outcome: "error", count: 1}},
			wantErrors: map[string]int64{"fetch": 1},
		},
		{
			name: "Fail records even when the underlying Fail errors, and passes the error through",
			run: func(t *testing.T, h metricsHarness) {
				t.Helper()
				job := testJob("l4", "api-src")
				failErr := errors.New("lease lost")
				h.jobs.EXPECT().Acquire(ctx, "api-src", time.Minute).Return(job, nil)
				h.jobs.EXPECT().Fail(ctx, job, persistence.FailureStorage).Return(failErr)
				_, err := h.sut.Acquire(ctx, "api-src", time.Minute)
				require.NoError(t, err)
				assert.Same(t, failErr, h.sut.Fail(ctx, job, persistence.FailureStorage))
			},
			wantSyncs:  []recordedSync{{source: "api-src", outcome: "error", count: 1}},
			wantErrors: map[string]int64{"storage": 1},
		},
		{
			name: "failed Complete records nothing and a following Fail records the attempt once",
			run: func(t *testing.T, h metricsHarness) {
				t.Helper()
				job := testJob("l5", "git-src")
				conflict := persistence.ErrConflict
				h.jobs.EXPECT().ClaimNext(ctx, time.Minute).Return(job, true, nil)
				h.jobs.EXPECT().Complete(ctx, job, result).Return(conflict)
				h.jobs.EXPECT().Fail(ctx, job, persistence.FailureValidation).Return(nil)
				_, _, err := h.sut.ClaimNext(ctx, time.Minute)
				require.NoError(t, err)
				assert.ErrorIs(t, h.sut.Complete(ctx, job, result), conflict)
				require.NoError(t, h.sut.Fail(ctx, job, persistence.FailureValidation))
			},
			wantSyncs:  []recordedSync{{source: "git-src", outcome: "error", count: 1}},
			wantErrors: map[string]int64{"validation": 1},
		},
		{
			name: "failed CommitSnapshot records nothing",
			run: func(t *testing.T, h metricsHarness) {
				t.Helper()
				job := testJob("l6", "git-src")
				h.jobs.EXPECT().ClaimNext(ctx, time.Minute).Return(job, true, nil)
				h.jobs.EXPECT().CommitSnapshot(ctx, job, result, snapshot).Return(persistence.ErrStale)
				_, _, err := h.sut.ClaimNext(ctx, time.Minute)
				require.NoError(t, err)
				assert.ErrorIs(t, h.sut.CommitSnapshot(ctx, job, result, snapshot), persistence.ErrStale)
			},
		},
		{
			name: "no claim means no start, so a later finish records nothing",
			run: func(t *testing.T, h metricsHarness) {
				t.Helper()
				job := testJob("l7", "git-src")
				h.jobs.EXPECT().ClaimNext(ctx, time.Minute).Return(persistence.Job{}, false, nil)
				h.jobs.EXPECT().Complete(ctx, job, result).Return(nil)
				h.jobs.EXPECT().Fail(ctx, job, persistence.FailureFetch).Return(nil)
				_, ok, err := h.sut.ClaimNext(ctx, time.Minute)
				require.NoError(t, err)
				require.False(t, ok)
				require.NoError(t, h.sut.Complete(ctx, job, result))
				require.NoError(t, h.sut.Fail(ctx, job, persistence.FailureFetch))
			},
		},
		{
			name: "ClaimNext error passes through and starts nothing",
			run: func(t *testing.T, h metricsHarness) {
				t.Helper()
				job := testJob("l8", "git-src")
				claimErr := errors.New("db down")
				h.jobs.EXPECT().ClaimNext(ctx, time.Minute).Return(job, true, claimErr)
				h.jobs.EXPECT().Complete(ctx, job, result).Return(nil)
				got, ok, err := h.sut.ClaimNext(ctx, time.Minute)
				assert.Same(t, claimErr, err)
				assert.True(t, ok, "results pass through unchanged")
				assert.Equal(t, job, got)
				require.NoError(t, h.sut.Complete(ctx, job, result))
			},
		},
		{
			name: "Acquire error passes through and starts nothing",
			run: func(t *testing.T, h metricsHarness) {
				t.Helper()
				job := testJob("l9", "k8s-src")
				h.jobs.EXPECT().Acquire(ctx, "k8s-src", time.Minute).Return(job, persistence.ErrBusy)
				h.jobs.EXPECT().Fail(ctx, job, persistence.FailureCancelled).Return(nil)
				got, err := h.sut.Acquire(ctx, "k8s-src", time.Minute)
				assert.ErrorIs(t, err, persistence.ErrBusy)
				assert.Equal(t, job, got)
				require.NoError(t, h.sut.Fail(ctx, job, persistence.FailureCancelled))
			},
		},
		{
			name: "a lease is recorded at most once",
			run: func(t *testing.T, h metricsHarness) {
				t.Helper()
				job := testJob("l10", "git-src")
				h.jobs.EXPECT().ClaimNext(ctx, time.Minute).Return(job, true, nil)
				h.jobs.EXPECT().Complete(ctx, job, result).Return(nil).Times(2)
				_, _, err := h.sut.ClaimNext(ctx, time.Minute)
				require.NoError(t, err)
				require.NoError(t, h.sut.Complete(ctx, job, result))
				require.NoError(t, h.sut.Complete(ctx, job, result))
			},
			wantSyncs: []recordedSync{{source: "git-src", outcome: "success", count: 1}},
		},
		{
			name: "concurrent leases are tracked independently",
			run: func(t *testing.T, h metricsHarness) {
				t.Helper()
				a, b := testJob("la", "src-a"), testJob("lb", "src-b")
				h.jobs.EXPECT().Acquire(ctx, "src-a", time.Minute).Return(a, nil)
				h.jobs.EXPECT().Acquire(ctx, "src-b", time.Minute).Return(b, nil)
				h.jobs.EXPECT().Complete(ctx, b, result).Return(nil)
				h.jobs.EXPECT().Fail(ctx, a, persistence.FailureFetch).Return(nil)
				_, err := h.sut.Acquire(ctx, "src-a", time.Minute)
				require.NoError(t, err)
				_, err = h.sut.Acquire(ctx, "src-b", time.Minute)
				require.NoError(t, err)
				require.NoError(t, h.sut.Complete(ctx, b, result))
				require.NoError(t, h.sut.Fail(ctx, a, persistence.FailureFetch))
			},
			wantSyncs: []recordedSync{
				{source: "src-a", outcome: "error", count: 1},
				{source: "src-b", outcome: "success", count: 1},
			},
			wantErrors: map[string]int64{"fetch": 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newMetricsHarness(t)
			tt.run(t, h)
			assert.ElementsMatch(t, tt.wantSyncs, h.syncs(t))
			if tt.wantErrors == nil {
				assert.Empty(t, h.syncErrors(t))
			} else {
				assert.Equal(t, tt.wantErrors, h.syncErrors(t))
			}
		})
	}
}

func TestMeasuredJobs_DelegatesUnwrappedMethods(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newMetricsHarness(t)
	job := testJob("l1", "git-src")
	renewed := job
	renewed.ExpiresAt = job.ExpiresAt.Add(time.Minute)
	status := persistence.JobStatus{SourceID: "id-git-src", Phase: persistence.JobInProgress, Attempts: 2}

	h.jobs.EXPECT().Renew(ctx, job, time.Minute).Return(renewed, nil)
	h.jobs.EXPECT().Status(ctx, "git-src").Return(status, nil)

	got, err := h.sut.Renew(ctx, job, time.Minute)
	require.NoError(t, err)
	assert.Equal(t, renewed, got)
	gotStatus, err := h.sut.Status(ctx, "git-src")
	require.NoError(t, err)
	assert.Equal(t, status, gotStatus)
	assert.Empty(t, h.syncs(t), "Renew and Status are not measured")
}

func TestMeasuredJobs_DurationCoversTheAttempt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newMetricsHarness(t)
	job := testJob("l1", "git-src")
	h.jobs.EXPECT().ClaimNext(ctx, time.Minute).Return(job, true, nil)
	h.jobs.EXPECT().Complete(ctx, job, gomock.Any()).Return(nil)

	_, _, err := h.sut.ClaimNext(ctx, time.Minute)
	require.NoError(t, err)
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, h.sut.Complete(ctx, job, persistence.SyncResult{}))

	m, ok := findMetric(h.collect(t), syncDurationMetric)
	require.True(t, ok)
	hist, ok := m.Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, hist.DataPoints, 1)
	assert.GreaterOrEqual(t, hist.DataPoints[0].Sum, 0.02, "duration runs from claim to completion, in seconds")
}

func TestMeasuredJobs_ConcurrentUse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newMetricsHarness(t)
	const workers = 20
	h.jobs.EXPECT().Acquire(ctx, gomock.Any(), time.Minute).DoAndReturn(
		func(_ context.Context, name string, _ time.Duration) (persistence.Job, error) {
			return testJob("lease-"+name, name), nil
		}).Times(workers)
	h.jobs.EXPECT().Complete(ctx, gomock.Any(), gomock.Any()).Return(nil).Times(workers)

	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := "src-" + string(rune('a'+i))
			job, err := h.sut.Acquire(ctx, name, time.Minute)
			assert.NoError(t, err)
			assert.NoError(t, h.sut.Complete(ctx, job, persistence.SyncResult{}))
		}()
	}
	wg.Wait()

	var total uint64
	for _, s := range h.syncs(t) {
		assert.Equal(t, "success", s.outcome)
		total += s.count
	}
	assert.Equal(t, uint64(workers), total)
	assert.Empty(t, h.sut.started)
}

func TestTracedFetcher(t *testing.T) {
	t.Parallel()
	def := persistence.SourceDefinition{Name: "git-src", Git: &persistence.GitSpec{Repository: "https://example.com/r.git"}}
	fetchErr := errors.New("fetch failed: secret-token-in-url")

	tests := []struct {
		name       string
		result     ingest.Result
		err        error
		wantStatus codes.Code
	}{
		{name: "success leaves status unset", result: ingest.Result{Hash: "abc"}, wantStatus: codes.Unset},
		{name: "failure marks the span as error", err: fetchErr, wantStatus: codes.Error},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

			raw := reconcilemocks.NewMockFetcher(gomock.NewController(t))
			raw.EXPECT().Fetch(gomock.Any(), def).DoAndReturn(
				func(ctx context.Context, _ persistence.SourceDefinition) (ingest.Result, error) {
					assert.True(t, trace.SpanFromContext(ctx).SpanContext().IsValid(), "the raw fetcher runs inside the span")
					return tt.result, tt.err
				})

			f := tracedFetcher{raw: raw, tracer: provider.Tracer("registry.sync")}
			got, err := f.Fetch(context.Background(), def)
			assert.Equal(t, tt.result, got)
			if tt.err != nil {
				assert.Same(t, tt.err, err, "the fetch error passes through unchanged")
			} else {
				require.NoError(t, err)
			}

			spans := recorder.Ended()
			require.Len(t, spans, 1)
			span := spans[0]
			assert.Equal(t, "sync.fetch", span.Name())
			assert.Contains(t, span.Attributes(), attribute.String("source", "git-src"))
			assert.Equal(t, tt.wantStatus, span.Status().Code)
			assert.NotContains(t, span.Status().Description, "secret", "the span never carries the raw error text")
		})
	}
}
