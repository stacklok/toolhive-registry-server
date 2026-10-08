package model_test

import (
	"slices"
	"testing"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
)

func TestCompareVersionsTotalOrder(t *testing.T) {
	t.Parallel()
	versions := []string{"custom-z", "", "custom-a", "1", "1.0", "1.0.0-alpha", "1.0.0", "v1.0.0", "1.0.0+z", "1.0.0+a", "2.0.0"}
	for _, a := range versions {
		for _, b := range versions {
			got := model.CompareVersions(a, b)
			if got != -model.CompareVersions(b, a) {
				t.Fatalf("antisymmetry: %q %q", a, b)
			}
			if (got == 0) != (a == b) {
				t.Fatalf("exact equality: %q %q: %d", a, b, got)
			}
			for _, c := range versions {
				if got <= 0 && model.CompareVersions(b, c) <= 0 && model.CompareVersions(a, c) > 0 {
					t.Fatalf("transitivity: %q <= %q <= %q", a, b, c)
				}
			}
		}
	}
	for _, tc := range []struct{ a, b string }{
		{"1", "1.0"}, {"1.0", "1.0.0"}, {"1", "v1.0.0"},
	} {
		if model.CompareVersions(tc.a, tc.b) >= 0 {
			t.Fatalf("NewVersion coercion should tie semantically then sort raw: %q < %q", tc.a, tc.b)
		}
	}
	if model.CompareVersions("1", "custom-z") <= 0 {
		t.Fatal("Masterminds accepts shortened versions")
	}

	want := slices.Clone(versions)
	slices.SortFunc(want, model.CompareVersions)
	for shift := range versions {
		permutation := append(slices.Clone(versions[shift:]), versions[:shift]...)
		slices.SortFunc(permutation, model.CompareVersions)
		if !slices.Equal(want, permutation) {
			t.Fatalf("permutation %d: %v != %v", shift, permutation, want)
		}
	}
	if want[len(want)-1] != "2.0.0" || want[0] != "" || model.CompareVersions("1.0.0", "custom-z") <= 0 {
		t.Fatalf("unexpected ranking: %v", want)
	}
}
