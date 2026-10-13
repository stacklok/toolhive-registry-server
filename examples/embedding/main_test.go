package main

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
)

func TestHostAdmission(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		state  *tls.ConnectionState
		status int
	}{
		{"plaintext", nil, http.StatusUnauthorized},
		{"unverified", &tls.ConnectionState{}, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler := authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				allow, err := (readAuthorizer{}).Authorize(r.Context(), httpapi.ConsumerRoutes()[0])
				require.NoError(t, err)
				require.True(t, allow)
				w.WriteHeader(http.StatusNoContent)
			}))
			r := httptest.NewRequest(http.MethodGet, "/registry/default/v0.1/servers", nil)
			r.TLS = tc.state // Evidence seam only; TLS certificate validation is Go's responsibility.
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			require.Equal(t, tc.status, w.Code)
		})
	}
	ctx := context.WithValue(context.Background(), verifiedClientKey{}, true)
	allow, err := (readAuthorizer{}).Authorize(ctx, httpapi.AdminRoutes()[0])
	require.NoError(t, err)
	require.False(t, allow)
	allow, err = (readAuthorizer{}).Authorize(context.Background(), httpapi.ConsumerRoutes()[0])
	require.NoError(t, err)
	require.False(t, allow)
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	allow, err = (readAuthorizer{}).Authorize(ctx, httpapi.ConsumerRoutes()[0])
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, allow)
}

func TestServeCancelsAndJoinsRequests(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, joined := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-r.Context().Done()
			w.WriteHeader(http.StatusNoContent)
			close(joined)
		}))
	}()
	clientDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			_, err = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		clientDone <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
	select {
	case <-joined:
	default:
		t.Fatal("server returned before request joined")
	}
	require.NoError(t, <-clientDone)
}
