package ingest

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// WithCause returns err with a redacted description of cause attached for host
// logs. The result's message is err's message alone and it satisfies
// errors.Is for err, so callers that return or display it never expose the
// cause; Cause recovers the description. A nil cause returns err unchanged.
func WithCause(err, cause error) error {
	if err == nil || cause == nil {
		return err
	}
	return &causeError{err: err, cause: RedactURLs(cause.Error())}
}

// Cause returns the redacted cause attached by WithCause anywhere in err's
// chain, or "" when there is none. It is meant for logs only.
func Cause(err error) string {
	var detailed *causeError
	if errors.As(err, &detailed) {
		return detailed.cause
	}
	return ""
}

// urlPattern matches absolute URLs inside free text, such as error messages.
var urlPattern = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"'<>]+`)

// RedactURLs removes user information (user names, passwords, tokens), query
// strings and fragments from every absolute URL in s, so the text can be logged.
// A URL that cannot be parsed is replaced entirely.
func RedactURLs(s string) string {
	return urlPattern.ReplaceAllStringFunc(s, func(raw string) string {
		trailing := ""
		for strings.HasSuffix(raw, ":") || strings.HasSuffix(raw, ",") || strings.HasSuffix(raw, ".") ||
			strings.HasSuffix(raw, ")") || strings.HasSuffix(raw, ";") {
			trailing = raw[len(raw)-1:] + trailing
			raw = raw[:len(raw)-1]
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" && parsed.Opaque != "" {
			return "[redacted URL]" + trailing
		}
		parsed.User = nil
		parsed.RawQuery, parsed.ForceQuery = "", false
		parsed.Fragment, parsed.RawFragment = "", ""
		return parsed.String() + trailing
	})
}

// causeError keeps a safe error as its message and a redacted cause for logs.
type causeError struct {
	err   error
	cause string
}

func (e *causeError) Error() string { return e.err.Error() }
func (e *causeError) Unwrap() error { return e.err }
