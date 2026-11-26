package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestWithAnonymousAdmission(t *testing.T) {
	t.Parallel()
	rejectAll := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	}
	for _, tc := range []struct {
		name          string
		mode          config.AuthMode
		allow, header bool
		wantStatus    int
		wantAnonymous bool
	}{
		{"anonymous mode admits every request", config.AuthModeAnonymous, true, true, http.StatusOK, true},
		{"oauth without header and opt-in is anonymous", config.AuthModeOAuth, true, false, http.StatusOK, true},
		{"oauth token is always validated", config.AuthModeOAuth, true, true, http.StatusUnauthorized, false},
		{"oauth without opt-in requires a token", config.AuthModeOAuth, false, false, http.StatusUnauthorized, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var anonymous bool
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				anonymous = IsAnonymous(r.Context())
				w.WriteHeader(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodGet, "/registry/main/v0.1/servers", nil)
			if tc.header {
				req.Header.Set("Authorization", "Bearer invalid")
			}
			rr := httptest.NewRecorder()
			WithAnonymousAdmission(rejectAll, tc.mode, tc.allow)(next).ServeHTTP(rr, req)
			assert.Equal(t, tc.wantStatus, rr.Code)
			assert.Equal(t, tc.wantAnonymous, anonymous)
		})
	}
	assert.False(t, IsAnonymous(httptest.NewRequest(http.MethodGet, "/", nil).Context()))
}
