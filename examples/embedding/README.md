# Embed a read-only PostgreSQL catalog

This runnable host mounts `postgres.NewEntries` → `catalog.NewReader` →
`httpapi.ConsumerRouter` at `/registry`. It uses Go's TLS verifier for mutual TLS
and the public `httpapi.Authorizer` seam for admission. It imports no registry
`internal` package and adds no dependencies beyond those already in the module.

## Prerequisites

- Go 1.26 or later; run commands below from the repository root.
- An existing PostgreSQL database at **clean schema 30**, with a named view such
  as `default` and its source definitions already present. Prepare a new catalog
  through [database setup](../../docs/database.md#setup-guide), or upgrade existing
  data through the [offline migration procedure](../../docs/registry-exposure-migration.md#maintenance-procedure).
  Initialize definitions/data using the standalone deployment or the
  [public definition and entry APIs](../../docs/registry-persistence.md).
  This example does not migrate, initialize definitions, fetch or publish entries.
- A database login that can read catalog tables and perform the latest-pointer
  repair done by `NewEntries` on startup. HTTP is read-only, **not** the database
  role. Use a verified PostgreSQL TLS connection outside local development.
- A server certificate/key, a CA file trusted by the calling client, and a
  **dedicated client CA** with client-auth certificates issued only to permitted
  catalog readers. The server certificate needs a DNS SAN for the requested hostname
  (`localhost` below) and the `serverAuth` extended key usage (EKU); client
  certificates need the `clientAuth` EKU. Obtain these through your host's certificate
  tooling; there are no bundled credentials or automatic production certificates.
- A private mode-0600 file outside the repository containing your PostgreSQL
  connection string. Supply its absolute path; do not put a credential-bearing
  DSN on the command line or in logs. Private keys also belong outside the repo.

## Run and query

Set these environment variables to **file paths**, not their contents:
`REGISTRY_DSN_FILE`, `SERVER_CERT_FILE`, `SERVER_KEY_FILE`, `CLIENT_CA_FILE`,
`SERVER_CA_FILE`, `CLIENT_CERT_FILE`, and `CLIENT_KEY_FILE`.

```sh
go run ./examples/embedding \
  -dsn-file "${REGISTRY_DSN_FILE:?Set absolute connection string file path}" \
  -tls-cert "${SERVER_CERT_FILE:?Set server certificate path}" \
  -tls-key "${SERVER_KEY_FILE:?Set server key path}" \
  -client-ca "${CLIENT_CA_FILE:?Set dedicated client CA path}"
```

In a second terminal:

```sh
curl --fail-with-body \
  --cacert "${SERVER_CA_FILE:?Set trusted server CA path}" \
  --cert "${CLIENT_CERT_FILE:?Set client certificate path}" \
  --key "${CLIENT_KEY_FILE:?Set client key path}" \
  https://localhost:8443/registry/default/v0.1/servers
```

Replace `default` with an existing view name. An existing empty view returns an
empty page; a missing view returns 404. Skills/plugins use the same
[consumer routes](../../docs/registry-consumer-http.md#compatibility-at-the-http-edge).
Run `go run ./examples/embedding -help` for flags, including `-listen`.
An external host can copy `main.go` into its own Go module and depend on this
module at its chosen version; the source uses only public constructors.

## Access and ownership

The listener defaults to `127.0.0.1:8443`; there is **no anonymous mode**. Go TLS
requires and verifies a client certificate before HTTP. Authentication middleware
passes verified evidence in private host context; the authorizer admits only
consumer GET descriptors. Every certificate trusted by the dedicated client CA
can read **every named view**, with the same nonpersonalized selection. Views are
not privacy boundaries. No admin, probes, metrics, OAuth, Cedar or audit sink is
mounted. This is a bounded embedding tutorial, not the standalone security stack.
If your host needs narrower audiences, replace admission/selection coherently and
add safe audit/telemetry; do not treat arbitrary request headers as verified identity.

The host opens and closes its pool. Startup is bounded to 30 seconds; server
header/read/write/idle timeouts are 5/10/15/60 seconds. Each request has a
10-second context deadline. SIGINT/SIGTERM cancels in-flight request work, stops
accepting connections, attempts HTTP shutdown for 10 seconds, then closes remaining
connections if necessary. It joins handlers before closing the pool. Replacement
handlers/backends must honor cancellation: a handler that ignores it can delay
shutdown beyond the grace period. Errors identify the failed stage without logging
a connection string, raw driver error, certificate identity or request payload.

## Compile and smoke test without PostgreSQL

```sh
task gen
go test -race -count=1 ./examples/embedding
go build -o /dev/null ./examples/embedding
```

The tests exercise real ephemeral-CA mTLS handshakes (trusted, absent, untrusted
and wrong-EKU client certificates), host admission and cancellation/joining on
isolated loopback listeners. They do not connect to PostgreSQL or Kubernetes.

For an isolated full-application check with disposable embedded PostgreSQL at
schema 30, public definition/entry seeding and ephemeral certificates:

```sh
go test -race -tags integration -count=1 ./examples/embedding
```

This calls the application's `run` path, verifies the named-view response over
hostname-verified mTLS, rejects a no-certificate request, and checks shutdown.
It never uses an environment DSN, a user database or a Kubernetes cluster; the
first run may download the suite's embedded PostgreSQL binaries. Running against
your own prepared database still requires the prerequisites above. See the
[embedding guide](../../docs/registry-embedding-validation.md) for focused contracts
and separate-module backend/HTTP conformance tests.
