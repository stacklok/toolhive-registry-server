package catalog_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog/mocks"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

func TestDefinitionAdminConstruction(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	sources, views := mocks.NewMockSources(ctrl), mocks.NewMockViews(ctrl)
	for _, deps := range []struct {
		s persistence.Sources
		v persistence.Views
	}{{nil, views}, {sources, nil}, {nil, nil}} {
		if _, err := catalog.NewDefinitionAdmin(deps.s, deps.v, catalog.DefinitionAdminOptions{}); !errors.Is(err, persistence.ErrInvalid) {
			t.Fatal(err)
		}
	}
	admin, err := catalog.NewDefinitionAdmin(sources, views, catalog.DefinitionAdminOptions{
		SyncInline:    func(context.Context, string) error { t.Fatal("constructor sync"); return nil },
		NotifyRefresh: func(context.Context) error { t.Fatal("constructor refresh"); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := admin.(persistence.Reconciler); ok {
		t.Fatal("admin exposes config reconciliation")
	}
	if _, ok := admin.(interface{ Close() }); ok {
		t.Fatal("admin owns borrowed storage")
	}
}

func TestDefinitionAdminFollowUp(t *testing.T) {
	t.Parallel()
	secret := errors.New("postgres://user:secret@private/callback")
	for _, operation := range []string{"create", "update", "delete"} {
		for _, failures := range []struct {
			name            string
			inline, refresh error
		}{
			{"success", nil, nil}, {"inline", secret, nil}, {"refresh", nil, context.DeadlineExceeded},
			{"both", secret, context.Canceled}, {"conflict", persistence.ErrConflict, nil},
		} {
			t.Run(operation+"/"+failures.name, func(t *testing.T) {
				t.Parallel()
				ctx := t.Context()
				sources, views := mocks.NewMockSources(gomock.NewController(t)), mocks.NewMockViews(gomock.NewController(t))
				request := persistence.SourceDefinition{Name: "request", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"}
				stored := persistence.SourceDefinition{Name: "persisted", ID: "incarnation", Origin: persistence.OriginAPI, File: &persistence.FileSpec{Data: "private"}, CreatedAt: time.Now().UTC()}
				calls := []string{}
				switch operation {
				case "create":
					sources.EXPECT().CreateSource(ctx, request).DoAndReturn(func(context.Context, persistence.SourceDefinition) (persistence.SourceDefinition, error) {
						calls = append(calls, "persist")
						return stored, nil
					})
				case "update":
					sources.EXPECT().UpdateSource(ctx, request.Name, request).DoAndReturn(func(context.Context, string, persistence.SourceDefinition) (persistence.SourceDefinition, error) {
						calls = append(calls, "persist")
						return stored, nil
					})
				case "delete":
					sources.EXPECT().DeleteSource(ctx, request.Name).DoAndReturn(func(context.Context, string) error { calls = append(calls, "persist"); return nil })
				}
				admin, err := catalog.NewDefinitionAdmin(sources, views, catalog.DefinitionAdminOptions{
					SyncInline: func(gotCtx context.Context, name string) error {
						if gotCtx != ctx || name != stored.Name || !reflect.DeepEqual(calls, []string{"persist"}) {
							t.Fatal("inline before persistence or detached request")
						}
						calls = append(calls, "inline")
						return failures.inline
					},
					NotifyRefresh: func(gotCtx context.Context) error {
						if gotCtx != ctx {
							t.Fatal("changed context")
						}
						calls = append(calls, "refresh")
						return failures.refresh
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				var got persistence.SourceDefinition
				switch operation {
				case "create":
					got, err = admin.CreateSource(ctx, request)
				case "update":
					got, err = admin.UpdateSource(ctx, request.Name, request)
				case "delete":
					err = admin.DeleteSource(ctx, request.Name)
				}
				wantCalls := []string{"persist", "inline", "refresh"}
				inlineErr := failures.inline
				if operation == "delete" {
					wantCalls = []string{"persist", "refresh"}
					inlineErr = nil
				} else if !reflect.DeepEqual(got, stored) {
					t.Fatal("not stored value")
				}
				if !reflect.DeepEqual(calls, wantCalls) {
					t.Fatal(calls)
				}
				var committed *catalog.PostCommitError
				if inlineErr == nil && failures.refresh == nil {
					if err != nil {
						t.Fatal(err)
					}
					return
				}
				if !errors.As(err, &committed) || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "postgres") {
					t.Fatalf("unsafe/untyped post-commit error: %v", err)
				}
				for _, stage := range []struct{ category, cause error }{{catalog.ErrSyncInline, inlineErr}, {catalog.ErrNotifyRefresh, failures.refresh}} {
					if errors.Is(err, stage.category) != (stage.cause != nil) || stage.cause != nil && !errors.Is(err, stage.cause) {
						t.Fatal("stage/cause lost")
					}
				}
			})
		}
	}
}

func TestDefinitionAdminDelegationAndStorageFailures(t *testing.T) {
	t.Parallel()
	for _, origin := range []persistence.Origin{persistence.OriginAPI, persistence.OriginConfig} {
		t.Run(string(origin), func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			sources, views := mocks.NewMockSources(ctrl), mocks.NewMockViews(ctrl)
			s := persistence.SourceDefinition{Name: "source", Origin: origin, File: &persistence.FileSpec{Data: "{}"}}
			v := persistence.ViewDefinition{Name: "view", Origin: origin, Sources: []string{s.Name}}
			ctx := t.Context()
			gomock.InOrder(
				sources.EXPECT().GetSource(ctx, s.Name).Return(s, nil), sources.EXPECT().ListSources(ctx).Return([]persistence.SourceDefinition{s}, nil),
				views.EXPECT().GetView(ctx, v.Name).Return(v, nil), views.EXPECT().ListViews(ctx).Return([]persistence.ViewDefinition{v}, nil),
				views.EXPECT().CreateView(ctx, v).Return(v, nil), views.EXPECT().UpdateView(ctx, v.Name, v).Return(v, nil), views.EXPECT().DeleteView(ctx, v.Name).Return(nil),
			)
			admin, err := catalog.NewDefinitionAdmin(sources, views, catalog.DefinitionAdminOptions{
				SyncInline:    func(context.Context, string) error { t.Fatal("unexpected sync"); return nil },
				NotifyRefresh: func(context.Context) error { t.Fatal("unexpected refresh"); return nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			gotS, err := admin.GetSource(ctx, s.Name)
			if err != nil || !reflect.DeepEqual(gotS, s) {
				t.Fatal(err)
			}
			ss, err := admin.ListSources(ctx)
			if err != nil || !reflect.DeepEqual(ss, []persistence.SourceDefinition{s}) {
				t.Fatal(err)
			}
			gotV, err := admin.GetView(ctx, v.Name)
			if err != nil || !reflect.DeepEqual(gotV, v) {
				t.Fatal(err)
			}
			vs, err := admin.ListViews(ctx)
			if err != nil || !reflect.DeepEqual(vs, []persistence.ViewDefinition{v}) {
				t.Fatal(err)
			}
			gotV, err = admin.CreateView(ctx, v)
			if err != nil || !reflect.DeepEqual(gotV, v) {
				t.Fatal(err)
			}
			gotV, err = admin.UpdateView(ctx, v.Name, v)
			if err != nil || !reflect.DeepEqual(gotV, v) {
				t.Fatal(err)
			}
			if err = admin.DeleteView(ctx, v.Name); err != nil {
				t.Fatal(err)
			}
			for _, failure := range []error{persistence.ErrAlreadyExists, persistence.ErrConfigOwned, persistence.ErrKindChange, persistence.ErrConflict, persistence.ErrNotFound, persistence.ErrInUse, persistence.ErrInvalid, persistence.ErrUnavailable, context.Canceled, context.DeadlineExceeded} {
				sources.EXPECT().CreateSource(ctx, s).Return(s, failure)
				sources.EXPECT().UpdateSource(ctx, s.Name, s).Return(s, failure)
				sources.EXPECT().DeleteSource(ctx, s.Name).Return(failure)
				views.EXPECT().CreateView(ctx, v).Return(v, failure)
				views.EXPECT().UpdateView(ctx, v.Name, v).Return(v, failure)
				views.EXPECT().DeleteView(ctx, v.Name).Return(failure)
				for _, op := range []func() error{
					func() error { _, e := admin.CreateSource(ctx, s); return e }, func() error { _, e := admin.UpdateSource(ctx, s.Name, s); return e }, func() error { return admin.DeleteSource(ctx, s.Name) },
					func() error { _, e := admin.CreateView(ctx, v); return e }, func() error { _, e := admin.UpdateView(ctx, v.Name, v); return e }, func() error { return admin.DeleteView(ctx, v.Name) },
				} {
					var post *catalog.PostCommitError
					if e := op(); !errors.Is(e, failure) || errors.As(e, &post) {
						t.Fatal("storage error changed")
					}
				}
			}
		})
	}
}

func TestDefinitionAdminOptionalCallbacksAndCancelledFollowUp(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"absent", "inline-only", "refresh-only", "not-inline", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			s := persistence.SourceDefinition{Name: "inline", File: &persistence.FileSpec{Data: "{}"}}
			if mode == "not-inline" {
				s.File = &persistence.FileSpec{Path: "/tmp/file"}
				s.Schedule = "1h"
			}
			sources, views := mocks.NewMockSources(gomock.NewController(t)), mocks.NewMockViews(gomock.NewController(t))
			sources.EXPECT().CreateSource(ctx, s).DoAndReturn(func(context.Context, persistence.SourceDefinition) (persistence.SourceDefinition, error) {
				if mode == "cancelled" {
					cancel()
				}
				return s, nil
			})
			inline, refresh := 0, 0
			options := catalog.DefinitionAdminOptions{}
			if mode != "absent" && mode != "refresh-only" {
				options.SyncInline = func(c context.Context, _ string) error { inline++; return c.Err() }
			}
			if mode != "absent" && mode != "inline-only" {
				options.NotifyRefresh = func(c context.Context) error { refresh++; return c.Err() }
			}
			admin, err := catalog.NewDefinitionAdmin(sources, views, options)
			if err != nil {
				t.Fatal(err)
			}
			got, err := admin.CreateSource(ctx, s)
			if !reflect.DeepEqual(got, s) {
				t.Fatal("lost committed value")
			}
			if mode == "cancelled" {
				var post *catalog.PostCommitError
				if !errors.As(err, &post) || !errors.Is(err, context.Canceled) || inline != 1 || refresh != 1 {
					t.Fatal("cancel hid committed outcome")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			wantInline, wantRefresh := 1, 1
			if mode == "absent" || mode == "refresh-only" || mode == "not-inline" {
				wantInline = 0
			}
			if mode == "absent" || mode == "inline-only" {
				wantRefresh = 0
			}
			if inline != wantInline || refresh != wantRefresh {
				t.Fatalf("callbacks: %d/%d", inline, refresh)
			}
		})
	}
}
