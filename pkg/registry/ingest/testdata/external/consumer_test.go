package consumer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
	"go.uber.org/mock/gomock"
)

// Sync is a complete host assembly with no database or listening server. The
// host supplies its persistence implementation and retains client ownership.
func Sync(ctx context.Context, jobs persistence.Jobs, sourceName string) error {
	coordinator, err := reconcile.New(jobs, ingest.New(ingest.Options{}), reconcile.Options{PollInterval: time.Second, LeaseDuration: time.Minute})
	if err != nil {
		return err
	}
	return coordinator.Sync(ctx, sourceName)
}

func TestUpstreamConstructor(t *testing.T) {
	upstream, err := ingest.NewUpstream(nil)
	if upstream != nil || !errors.Is(err, persistence.ErrInvalid) {
		t.Fatalf("nil client: upstream=%v, err=%v", upstream, err)
	}
}

func TestHostAssembly(t *testing.T) {
	jobs := mocks.NewMockJobs(gomock.NewController(t))
	source := persistence.SourceDefinition{ID: "host-source", Name: "inline", File: &persistence.FileSpec{Data: `{"version":"1.0.0","meta":{"last_updated":"2025-01-15T10:30:00Z"},"data":{"servers":[{"$schema":"https://example.org/server.schema.json","name":"com.example/server","description":"Server","version":"1"}],"skills":[{"namespace":"io.example","name":"skill","description":"Skill","version":"1"}],"plugins":[{"namespace":"io.example","name":"plugin","description":"Plugin","version":"1"}]}}`}}
	job := persistence.Job{Source: source, LeaseID: "host-lease", Generation: 1}
	jobs.EXPECT().Acquire(gomock.Any(), source.Name, time.Minute).Return(job, nil)
	jobs.EXPECT().Status(gomock.Any(), source.Name).Return(persistence.JobStatus{SourceID: source.ID}, nil)
	jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ persistence.Job, _ persistence.SyncResult, snapshot model.Snapshot) error {
		if len(snapshot.Data.Servers) != 1 || len(snapshot.Data.Skills) != 1 || len(snapshot.Data.Plugins) != 1 {
			t.Fatal("incomplete snapshot")
		}
		return nil
	})
	if err := Sync(t.Context(), jobs, source.Name); err != nil {
		t.Fatal(err)
	}
}
