package claims_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database/dbtest"
	"github.com/stacklok/toolhive-registry-server/internal/claims"
)

// visibilityCases is the shared table for the read-path rule. Every case runs
// against both the Go rule (VisibleJSON) and the SQL function claims_visible
// used by the claims-mode list query, so the two can never drift apart. A nil
// record means SQL NULL.
var visibilityCases = []struct {
	name   string
	caller string
	record *string
	want   bool
}{
	{"sql null record is hidden", `{"org":"acme"}`, nil, false},
	{"json null record is visible", `{"org":"acme"}`, ptr(`null`), true},
	{"empty object record is visible", `{"org":"acme"}`, ptr(`{}`), true},
	{"array record is hidden", `{"org":"acme"}`, ptr(`["acme"]`), false},
	{"string record is hidden", `{"org":"acme"}`, ptr(`"acme"`), false},
	{"number record is hidden", `{"org":"acme"}`, ptr(`1`), false},
	{"scalar match", `{"org":"acme"}`, ptr(`{"org":"acme"}`), true},
	{"scalar mismatch", `{"org":"acme"}`, ptr(`{"org":"other"}`), false},
	{"caller lacks key", `{"team":"eng"}`, ptr(`{"org":"acme"}`), false},
	{"extra caller keys ignored", `{"org":"acme","sub":"u1","exp":1}`, ptr(`{"org":"acme"}`), true},
	{"and across keys, all hold", `{"org":"acme","team":"eng"}`, ptr(`{"org":"acme","team":"eng"}`), true},
	{"and across keys, one fails", `{"org":"acme","team":"data"}`, ptr(`{"org":"acme","team":"eng"}`), false},
	{"record array overlaps scalar caller", `{"team":"eng"}`, ptr(`{"team":["eng","data"]}`), true},
	{"record array no overlap", `{"team":"ops"}`, ptr(`{"team":["eng","data"]}`), false},
	{"caller array overlaps record array", `{"team":["ops","data"]}`, ptr(`{"team":["eng","data"]}`), true},
	{"caller array overlaps scalar record", `{"org":["acme","community"]}`, ptr(`{"org":"acme"}`), true},
	{"empty record array needs key only", `{"team":"anything"}`, ptr(`{"team":[]}`), true},
	{"empty record array, key missing", `{"org":"acme"}`, ptr(`{"team":[]}`), false},
	{"empty record array, caller value null", `{"team":null}`, ptr(`{"team":[]}`), true},
	{"caller value null fails non-empty", `{"team":null}`, ptr(`{"team":"eng"}`), false},
	{"caller number value matches nothing", `{"level":3}`, ptr(`{"level":"3"}`), false},
	{"caller array ignores non-strings", `{"team":[1,"eng"]}`, ptr(`{"team":"eng"}`), true},
	{"record number value fails closed", `{"level":3}`, ptr(`{"level":3}`), false},
	{"record nested object fails closed", `{"org":{"a":"b"}}`, ptr(`{"org":{"a":"b"}}`), false},
	{"record null value fails closed", `{"org":null}`, ptr(`{"org":null}`), false},
	{"record mixed array fails closed", `{"team":"eng"}`, ptr(`{"team":["eng",1]}`), false},
	{"record bool value fails closed", `{"admin":true}`, ptr(`{"admin":true}`), false},
	{"case sensitive values", `{"org":"Acme"}`, ptr(`{"org":"acme"}`), false},
	{"empty string value", `{"org":""}`, ptr(`{"org":""}`), true},
}

func ptr(s string) *string { return &s }

//nolint:paralleltest,tparallel // subtests share one pgx.Conn, which is not safe for concurrent use
func TestVisibilityGoAndSQLAgree(t *testing.T) {
	t.Parallel()
	conn, cleanup := dbtest.SetupTestDB(t)
	t.Cleanup(cleanup)
	ctx := context.Background()
	for _, tc := range visibilityCases {
		t.Run(tc.name, func(t *testing.T) {
			var record []byte
			if tc.record != nil {
				record = []byte(*tc.record)
			}
			assert.Equal(t, tc.want, claims.VisibleJSON([]byte(tc.caller), record), "Go rule")

			var got bool
			require.NoError(t, conn.QueryRow(ctx, `SELECT claims_visible($1::jsonb, $2::jsonb)`,
				tc.caller, tc.record).Scan(&got))
			assert.Equal(t, tc.want, got, "SQL claims_visible")
		})
	}
}

func TestContainAndEqual(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name             string
		caller, record   map[string]any
		visible, contain bool
	}{
		{"read overlaps but write needs all", map[string]any{"team": []any{"eng"}}, map[string]any{"team": []any{"eng", "data"}}, true, false},
		{"caller holds all values", map[string]any{"team": []any{"eng", "data", "ops"}}, map[string]any{"team": []any{"eng", "data"}}, true, true},
		{"empty record array", map[string]any{"team": "x"}, map[string]any{"team": []any{}}, true, true},
		{"no record claims", map[string]any{"org": "acme"}, map[string]any{}, true, true},
		{"typed string slice", map[string]any{"org": []string{"acme"}}, map[string]any{"org": []string{"acme"}}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.visible, claims.Visible(tc.caller, tc.record))
			assert.Equal(t, tc.contain, claims.Contain(tc.caller, tc.record))
		})
	}
	assert.True(t, claims.Equal(map[string]any{"org": []any{"a", "b"}}, map[string]any{"org": []any{"b", "a"}}))
	assert.False(t, claims.Equal(map[string]any{"org": "a"}, map[string]any{"org": []any{"a", "b"}}))
}

func TestDeserialize(t *testing.T) {
	t.Parallel()
	assert.Nil(t, claims.Deserialize(nil))
	assert.Nil(t, claims.Deserialize([]byte(`null`)))
	assert.Nil(t, claims.Deserialize([]byte(`[1]`)))
	assert.Nil(t, claims.Deserialize([]byte(`{`)))
	assert.Equal(t, map[string]any{}, claims.Deserialize([]byte(`{}`)))
	assert.Equal(t, map[string]any{"org": "acme"}, claims.Deserialize([]byte(`{"org":"acme"}`)))
}

func TestViewCaller(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	assert.Nil(t, claims.ViewCaller(ctx))
	assert.Equal(t, ctx, claims.WithViewCaller(ctx, nil))
	assert.Equal(t, []byte(`{"org":"acme"}`), claims.ViewCaller(claims.WithViewCaller(ctx, []byte(`{"org":"acme"}`))))
}
