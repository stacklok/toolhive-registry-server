package persistence_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

func TestViewQueryValidation(t *testing.T) {
	t.Parallel()
	invalidTime := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		view  string
		query persistence.ViewQuery
	}{
		{"empty view", "", persistence.ViewQuery{Kind: persistence.SkillKind, Limit: 1}},
		{"zero bound", "view", persistence.ViewQuery{Kind: persistence.SkillKind}},
		{"oversized bound", "view", persistence.ViewQuery{Kind: persistence.SkillKind, Limit: 1001}},
		{"negative bound", "view", persistence.ViewQuery{Kind: persistence.SkillKind, Limit: -1}},
		{"invalid kind", "view", persistence.ViewQuery{Kind: "bad", Limit: 1}},
		{"both selectors", "view", persistence.ViewQuery{Kind: persistence.SkillKind, Limit: 1, VersionSelector: persistence.VersionSelector{Latest: true, Exact: "latest"}}},
		{"server namespace", "view", persistence.ViewQuery{Kind: persistence.ServerKind, Namespace: "com.example", Limit: 1}},
		{"oversized cursor", "view", persistence.ViewQuery{Kind: persistence.SkillKind, Limit: 1, Cursor: strings.Repeat("a", 4097)}},
		{"unencodable timestamp", "view", persistence.ViewQuery{Kind: persistence.SkillKind, Limit: 1, UpdatedSince: &invalidTime}},
		{"invalid utf8", "view", persistence.ViewQuery{Kind: persistence.SkillKind, Search: "\xff", Limit: 1}},
		{"nul filter", "view", persistence.ViewQuery{Kind: persistence.SkillKind, Search: "\x00", Limit: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if e := tc.query.Validate(tc.view); !errors.Is(e, persistence.ErrInvalid) {
				t.Fatal(e)
			}
		})
	}
	for _, kind := range []persistence.EntryKind{persistence.ServerKind, persistence.SkillKind, persistence.PluginKind} {
		for _, limit := range []int{1, 2, 1000} {
			q := persistence.ViewQuery{Kind: kind, Limit: limit, Search: strings.Repeat("x", 5000)}
			if e := q.Validate("view"); e != nil {
				t.Fatal(e)
			}
		}
	}
	for _, selector := range []persistence.VersionSelector{{Exact: "latest"}, {Latest: true}} {
		if e := (persistence.EntryName{Kind: persistence.SkillKind, Name: "item"}).Validate("view", selector); e != nil {
			t.Fatal(e)
		}
	}
}
