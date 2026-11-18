package catalog_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog/mocks"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

func TestPublicationDiagnosticsConstruction(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	backend := mocks.NewMockManagedPublisher(ctrl)
	entries := mocks.NewMockEntryDiagnostics(ctrl)
	status := mocks.NewMockSourceStatusReader(ctrl)
	if _, err := catalog.NewPublisher(nil); !errors.Is(err, persistence.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := catalog.NewPublisher(backend); err != nil {
		t.Fatal(err)
	}
	for _, deps := range []struct {
		entries persistence.EntryDiagnostics
		status  persistence.SourceStatusReader
	}{{nil, status}, {entries, nil}, {nil, nil}} {
		if _, err := catalog.NewDiagnostics(deps.entries, deps.status); !errors.Is(err, persistence.ErrInvalid) {
			t.Fatal(err)
		}
	}
	diag, err := catalog.NewDiagnostics(entries, status)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := diag.(persistence.Jobs); ok {
		t.Fatal("diagnostics exposes job mutation")
	}
	if _, ok := diag.(persistence.Sources); ok {
		t.Fatal("diagnostics exposes source configuration")
	}
}

func TestPublisherInputAndInjection(t *testing.T) {
	t.Parallel()
	for _, kind := range []persistence.EntryKind{persistence.ServerKind, persistence.SkillKind, persistence.PluginKind} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			backend := mocks.NewMockManagedPublisher(ctrl)
			publisher, err := catalog.NewPublisher(backend)
			if err != nil {
				t.Fatal(err)
			}
			for _, mutate := range []func(*persistence.Entry){
				func(e *persistence.Entry) { e.ID = "assigned" },
				func(e *persistence.Entry) { e.SourceID = "destination" },
				func(e *persistence.Entry) { e.IsLatest = true },
				func(e *persistence.Entry) { e.Name = "" },
				func(e *persistence.Entry) { e.Version = "" },
				func(e *persistence.Entry) { e.Name = "mismatch" },
				func(e *persistence.Entry) { e.Version = "mismatch" },
				func(e *persistence.Entry) { e.Kind = "unknown" },
				func(e *persistence.Entry) { e.Server, e.Skill, e.Plugin = nil, nil, nil },
				func(e *persistence.Entry) {
					e.Server = &model.Server{}
					e.Skill = &model.Skill{}
					e.Plugin = &model.Plugin{}
				},
				func(e *persistence.Entry) {
					if e.Server != nil {
						e.Server.Description = ""
					}
					if e.Skill != nil {
						e.Skill.Namespace = ""
					}
					if e.Plugin != nil {
						e.Plugin.Namespace = ""
					}
				},
				func(e *persistence.Entry) {
					if e.Server != nil {
						e.Server.Name = "not a server name"
					}
					if e.Skill != nil {
						e.Skill.ID = "assigned"
					}
					if e.Plugin != nil {
						e.Plugin.CreatedAt = time.Now()
					}
				},
			} {
				input := publisherInput(kind)
				mutate(&input)
				if _, err = publisher.Publish(t.Context(), input); !errors.Is(err, persistence.ErrInvalid) {
					t.Fatalf("invalid input reached backend: %+v / %v", input, err)
				}
			}
			input := publisherInput(kind)
			stored := input
			stored.ID, stored.SourceID = "assigned", "managed"
			backend.EXPECT().Publish(t.Context(), input).Return(stored, nil)
			got, err := publisher.Publish(t.Context(), input)
			if err != nil || !reflect.DeepEqual(got, stored) {
				t.Fatal(got, err)
			}
			for _, failure := range []error{persistence.ErrConflict, persistence.ErrNoManagedSource, persistence.ErrUnavailable, context.Canceled} {
				backend.EXPECT().Publish(t.Context(), input).Return(persistence.Entry{}, failure)
				if _, err = publisher.Publish(t.Context(), input); !errors.Is(err, failure) {
					t.Fatal(err)
				}
				backend.EXPECT().DeleteManaged(t.Context(), kind, input.Name, "latest").Return(failure)
				if err = publisher.DeleteManaged(t.Context(), kind, input.Name, "latest"); !errors.Is(err, failure) {
					t.Fatal(err)
				}
			}
			for _, input := range []struct {
				kind          persistence.EntryKind
				name, version string
			}{
				{"bad", "item", "1"}, {kind, "", "1"}, {kind, "item", ""},
			} {
				if err = publisher.DeleteManaged(t.Context(), input.kind, input.name, input.version); !errors.Is(err, persistence.ErrInvalid) {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestDiagnosticsIndependentInjection(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	entries := mocks.NewMockEntryDiagnostics(ctrl)
	status := mocks.NewMockSourceStatusReader(ctrl)
	diag, err := catalog.NewDiagnostics(entries, status)
	if err != nil {
		t.Fatal(err)
	}
	timestamp := time.Now().UTC()
	rows := []persistence.DiagnosticVersion{{Kind: persistence.SkillKind, Name: "item", Version: "1", CreatedAt: timestamp, UpdatedAt: timestamp}}
	view := []persistence.ViewDiagnosticVersion{{DiagnosticVersion: rows[0], SourceName: "a", SourceID: "a-incarnation"},
		{DiagnosticVersion: rows[0], SourceName: "b", SourceID: "b-incarnation", Position: 1}}
	entries.EXPECT().ListSourceVersions(t.Context(), "a").Return(rows, nil)
	got, err := diag.ListSourceVersions(t.Context(), "a")
	if err != nil || !reflect.DeepEqual(got, rows) {
		t.Fatal(got, err)
	}
	entries.EXPECT().ListViewVersions(t.Context(), "view").Return(view, nil)
	gotView, err := diag.ListViewVersions(t.Context(), "view")
	if err != nil || !reflect.DeepEqual(gotView, view) {
		t.Fatal(gotView, err)
	}
	for _, phase := range []persistence.JobPhase{persistence.JobPending, persistence.JobInProgress, persistence.JobCompleted, persistence.JobFailed} {
		want := persistence.JobStatus{SourceID: "authoritative-incarnation", Phase: phase, StartedAt: &timestamp, Failure: persistence.FailureStorage}
		status.EXPECT().Status(t.Context(), "source").Return(want, nil)
		actual, e := diag.Status(t.Context(), "source")
		if e != nil || !reflect.DeepEqual(actual, want) {
			t.Fatal(actual, e)
		}
	}
	for _, failure := range []error{persistence.ErrNotFound, persistence.ErrStale, persistence.ErrUnavailable, context.Canceled, context.DeadlineExceeded} {
		status.EXPECT().Status(t.Context(), "source").Return(persistence.JobStatus{}, failure)
		if _, err = diag.Status(t.Context(), "source"); !errors.Is(err, failure) {
			t.Fatal(err)
		}
		entries.EXPECT().ListSourceVersions(t.Context(), "a").Return(nil, failure)
		if _, err = diag.ListSourceVersions(t.Context(), "a"); !errors.Is(err, failure) {
			t.Fatal(err)
		}
		entries.EXPECT().ListViewVersions(t.Context(), "view").Return(nil, failure)
		if _, err = diag.ListViewVersions(t.Context(), "view"); !errors.Is(err, failure) {
			t.Fatal(err)
		}
	}
}

func publisherInput(kind persistence.EntryKind) persistence.Entry {
	entry := persistence.Entry{Kind: kind, Name: "item", Version: "1"}
	switch kind {
	case persistence.ServerKind:
		entry.Name = "com.example/item"
		entry.Server = &model.Server{Schema: "https://example.org/server.schema.json", Name: entry.Name, Version: entry.Version, Description: "server"}
	case persistence.SkillKind:
		entry.Skill = &model.Skill{Namespace: "com.example", Name: entry.Name, Version: entry.Version, Description: "skill"}
	case persistence.PluginKind:
		entry.Plugin = &model.Plugin{Namespace: "com.example", Name: entry.Name, Version: entry.Version, Description: "plugin"}
	}
	return entry
}
