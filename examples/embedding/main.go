// Command embedding serves an existing catalog through host-owned mTLS admission.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

type verifiedClientKey struct{}

type readAuthorizer struct{}

func (readAuthorizer) Authorize(ctx context.Context, route httpapi.RouteDescriptor) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	verified, _ := ctx.Value(verifiedClientKey{}).(bool)
	return verified && route.Method == http.MethodGet && route.RegistryParam == "registryName", nil
}

func authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
			http.Error(w, "client certificate required", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), verifiedClientKey{}, true)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func main() {
	addr := flag.String("listen", "127.0.0.1:8443", "HTTPS listen address")
	dsnFile := flag.String("dsn-file", "", "absolute path to a PostgreSQL connection string file")
	certFile := flag.String("tls-cert", "", "server certificate PEM file")
	keyFile := flag.String("tls-key", "", "server private key PEM file")
	caFile := flag.String("client-ca", "", "dedicated trusted client CA PEM file")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *addr, *dsnFile, *certFile, *keyFile, *caFile); err != nil {
		// Only fixed stage errors escape run; driver errors may contain credentials.
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context, addr, dsnFile, certFile, keyFile, caFile string) error {
	if !filepath.IsAbs(dsnFile) {
		return errors.New("dsn-file must be an absolute path")
	}
	resolved, err := filepath.EvalSymlinks(dsnFile)
	if err != nil {
		return errors.New("cannot resolve connection string file")
	}
	dsn, err := os.ReadFile(resolved)
	if err != nil || strings.TrimSpace(string(dsn)) == "" {
		return errors.New("cannot read a nonempty connection string")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return errors.New("cannot load server TLS certificate and key")
	}
	caPEM, err := os.ReadFile(caFile) //nolint:gosec // G304: explicit operator-owned CLI path, like the server key pair.
	if err != nil {
		return errors.New("cannot read client CA")
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(caPEM) {
		return errors.New("client CA contains no certificates")
	}
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(startup, strings.TrimSpace(string(dsn)))
	if err != nil {
		return errors.New("cannot configure PostgreSQL pool")
	}
	defer pool.Close()
	if err := pool.Ping(startup); err != nil {
		return errors.New("cannot connect to PostgreSQL")
	}
	// Clean schema 30 is required. NewEntries repairs latest pointers, so the
	// database role needs write privileges even though HTTP is read-only.
	entries, err := postgres.NewEntries(startup, pool, 65536)
	if err != nil {
		return errors.New("cannot initialize entries; check schema and repair privileges")
	}
	reader, err := catalog.NewReader(entries)
	if err != nil {
		return errors.New("cannot construct catalog reader")
	}
	consumer, err := httpapi.ConsumerRouter(reader, httpapi.ConsumerOptions{
		Middleware: []func(http.Handler) http.Handler{authenticate},
		Decorate:   httpapi.Authorize(readAuthorizer{}),
	})
	if err != nil {
		return errors.New("cannot construct consumer router")
	}
	mux := chi.NewRouter()
	mux.Mount("/registry", consumer)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.New("cannot bind HTTPS listener")
	}
	defer listener.Close()
	secure := tls.NewListener(listener, &tls.Config{
		MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert},
		ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: clientCAs,
	})
	fmt.Println("Serving mTLS consumer API on", listener.Addr())
	return serve(ctx, secure, mux)
}

func serve(ctx context.Context, listener net.Listener, handler http.Handler) error {
	work, cancelWork := context.WithCancel(ctx)
	defer cancelWork()
	var requests sync.WaitGroup
	var requestMu sync.Mutex
	server := &http.Server{
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
		BaseContext: func(net.Listener) context.Context { return work },
		// Suppress raw TLS/transport error logging; hosts can add safe telemetry.
		ErrorLog: log.New(io.Discard, "", 0),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestMu.Lock()
			if work.Err() != nil {
				requestMu.Unlock()
				http.Error(w, "shutting down", http.StatusServiceUnavailable)
				return
			}
			requests.Add(1)
			requestMu.Unlock()
			defer requests.Done()
			requestCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			handler.ServeHTTP(w, r.WithContext(requestCtx))
		}),
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-done:
		done = nil
	}
	// Serialize cancellation with handler admission so no work can be added
	// after the final Wait, including handlers arriving during forced close.
	requestMu.Lock()
	cancelWork()
	requestMu.Unlock()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shutdownErr := server.Shutdown(shutdown)
	if shutdownErr != nil {
		_ = server.Close()
	}
	if done != nil {
		serveErr = <-done
	}
	requests.Wait() // Join canceled requests before run closes the pool.
	if shutdownErr != nil {
		return errors.New("HTTP shutdown exceeded deadline")
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return errors.New("HTTP listener failed")
	}
	return nil
}
