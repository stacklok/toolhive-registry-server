package postgres

import (
	"testing"
	"time"
)

// v1.5.4 stored a schedule (and filter) on sources that cannot use them, such
// as an inline source created through the API with a syncPolicy. Such a row
// must load as a valid definition, or the sync runtime refuses every source.
func TestDecodeSourceDropsUnusableStoredSettings(t *testing.T) {
	t.Parallel()
	seven := int64(7 * time.Second / time.Microsecond)
	filter := []byte(`{"names":{"include":["a*"]}}`)
	for _, tc := range []struct {
		name         string
		kind         string
		spec, filter []byte
		schedule     string
		keepsFilter  bool
	}{
		{"inline file drops the schedule", "file", []byte(`{"data":"{}"}`), nil, "", false},
		{"managed drops both", "managed", nil, filter, "", false},
		{"kubernetes drops both", "kubernetes", []byte(`{"namespaces":["team"]}`), filter, "", false},
		{"polled file keeps both", "file", []byte(`{"path":"/registry.json"}`), filter, "7s", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, err := decodeSource("id", "src", "API", tc.kind, tc.spec, tc.filter, seven, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if s.Schedule != tc.schedule || (s.Filter != nil) != tc.keepsFilter {
				t.Fatalf("schedule %q filter %v; want %q keepsFilter=%v", s.Schedule, s.Filter, tc.schedule, tc.keepsFilter)
			}
			if _, err := s.Kind(); err != nil {
				t.Fatalf("loaded definition is invalid: %v", err)
			}
		})
	}
}
