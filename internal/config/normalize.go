package config

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/gobwas/glob"
)

// Compatibility normalization for source settings that v1.5.4 accepted but the
// registry library refuses. Each function returns the value the server stores
// (what v1.5.4 effectively did, or the nearest value the library can honor)
// and a warning describing the change. An empty warning means the value was
// left alone. Values that do not parse are left alone too: validation reports
// them.

const (
	// MinSyncInterval is the shortest sync interval the sync runtime can honor:
	// it checks for due sources once per second.
	MinSyncInterval = time.Second
	// LegacyNonPositiveSyncInterval is how often v1.5.4 synced a source whose
	// syncPolicy.interval was zero or negative: on every tick of its sync
	// coordinator, which ran every two minutes.
	LegacyNonPositiveSyncInterval = 2 * time.Minute
	// LegacyZeroFileTimeout is the per-request timeout v1.5.4 used for
	// file.timeout "0s": its HTTP client replaced a zero timeout with its 10s
	// default.
	LegacyZeroFileTimeout = 10 * time.Second
	// matchEverything is a name pattern that matches every name.
	matchEverything = "*"
)

// NormalizeSyncInterval returns the sync interval to store for a polled
// source. A zero or negative interval becomes LegacyNonPositiveSyncInterval
// (v1.5.4's effective cadence), an interval below MinSyncInterval becomes
// MinSyncInterval, and sub-microsecond precision, which v1.5.4's database
// interval column dropped, is truncated.
func NormalizeSyncInterval(interval string) (string, string) {
	d, err := time.ParseDuration(interval)
	if err != nil {
		return interval, ""
	}
	switch {
	case d <= 0:
		return LegacyNonPositiveSyncInterval.String(), fmt.Sprintf(
			"syncPolicy.interval %q is not positive; using %s, the cadence v1.5.4 synced such a source at",
			interval, LegacyNonPositiveSyncInterval)
	case d < MinSyncInterval:
		return MinSyncInterval.String(), fmt.Sprintf(
			"syncPolicy.interval %q is shorter than %s, the shortest interval the sync runtime honors; using %s",
			interval, MinSyncInterval, MinSyncInterval)
	case d%time.Microsecond != 0:
		truncated := d.Truncate(time.Microsecond)
		return truncated.String(), fmt.Sprintf(
			"syncPolicy.interval %q has sub-microsecond precision; using %s", interval, truncated)
	}
	return interval, ""
}

// NormalizeFileTimeout returns the file.timeout to store for a file source.
// A timeout on a path or inline source is dropped, as v1.5.4 ignored it. On a
// URL source, "0s" becomes LegacyZeroFileTimeout (v1.5.4's effective timeout)
// and a negative timeout, which disabled the timeout in v1.5.4, is dropped so
// the default applies: a request timeout can no longer be disabled.
func NormalizeFileTimeout(file *FileConfig) (string, string) {
	if file == nil || file.Timeout == "" {
		return "", ""
	}
	if file.URL == "" {
		return "", fmt.Sprintf(
			"file.timeout %q applies only to file.url sources; ignoring it, as v1.5.4 did", file.Timeout)
	}
	d, err := time.ParseDuration(file.Timeout)
	if err != nil {
		return file.Timeout, ""
	}
	switch {
	case d == 0:
		return LegacyZeroFileTimeout.String(), fmt.Sprintf(
			"file.timeout %q is zero; using %s, the timeout v1.5.4 applied for it", file.Timeout, LegacyZeroFileTimeout)
	case d < 0:
		return "", fmt.Sprintf(
			"file.timeout %q is negative, which disabled the request timeout in v1.5.4; "+
				"a timeout can no longer be disabled, so the default applies", file.Timeout)
	}
	return file.Timeout, ""
}

// NormalizeNameFilter rewrites a name filter that holds invalid glob patterns
// into the valid filter that selects exactly what v1.5.4 selected. v1.5.4
// tried patterns in order and treated an invalid pattern as a non-match that
// ended the search: an invalid exclude pattern excluded every name, and an
// invalid include pattern excluded every name that no earlier include pattern
// matched. A filter with only valid patterns is returned unchanged.
func NormalizeNameFilter(names *NameFilterConfig) (*NameFilterConfig, string) {
	if names == nil {
		return nil, ""
	}
	if bad, ok := firstInvalidPattern(names.Exclude); ok {
		return &NameFilterConfig{Exclude: []string{matchEverything}}, fmt.Sprintf(
			"filter.names.exclude pattern %q is not a valid glob; as in v1.5.4, the filter excludes every entry",
			names.Exclude[bad])
	}
	bad, ok := firstInvalidPattern(names.Include)
	if !ok {
		return names, ""
	}
	if bad == 0 {
		return &NameFilterConfig{Exclude: []string{matchEverything}}, fmt.Sprintf(
			"filter.names.include pattern %q is not a valid glob; as in v1.5.4, the filter excludes every entry",
			names.Include[bad])
	}
	kept := append([]string(nil), names.Include[:bad]...)
	return &NameFilterConfig{Include: kept, Exclude: names.Exclude}, fmt.Sprintf(
		"filter.names.include pattern %q is not a valid glob; as in v1.5.4, only the include patterns before it "+
			"(%q) apply and every other entry is excluded", names.Include[bad], kept)
}

// firstInvalidPattern returns the index of the first pattern v1.5.4 or the
// library would reject.
func firstInvalidPattern(patterns []string) (int, bool) {
	for i, p := range patterns {
		if !validNamePattern(p) {
			return i, true
		}
	}
	return 0, false
}

// validNamePattern applies v1.5.4's pattern check (filepath.Match against a
// sample name, then gobwas/glob) and the library's (filepath.Match against an
// empty name), so a pattern kept here is accepted by both.
func validNamePattern(pattern string) bool {
	if _, err := filepath.Match(pattern, "test"); err != nil {
		return false
	}
	if _, err := filepath.Match(pattern, ""); err != nil {
		return false
	}
	_, err := glob.Compile(pattern)
	return err == nil
}
