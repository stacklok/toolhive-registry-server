# Docker deployment

Use the repository's Compose setup for local development. It builds the current
source and includes PostgreSQL; it is not a production credential or TLS setup.
Run the commands below from the repository root with Docker Compose v2.

## Compose: prepare configuration

1. Install Docker/Compose and allow enough disk space for a Go image build and
   persistent PostgreSQL data.
2. In [examples/config-docker.yaml](../examples/config-docker.yaml), replace the
   OAuth provider's `issuerUrl` and `audience` with your trusted IdP settings.
   Admin stays off. Built-in allow-all Cedar admits authenticated users without
   group mappings. If you add mappings for a custom policy, their issuer must
   match a configured provider exactly.
3. Keep the supplied database host `postgres`, users `db_app`/`db_migrator`,
   database `registry`, and file path `/examples/upstream-registry.json` for this
   development setup. `CONFIG_FILE` selects another file under `/examples` in
   Compose; it is not a registry CLI environment variable.

[The Compose file](../docker-compose.yaml) mounts `examples` read-only and stores
PostgreSQL in `postgres_data`. The development image embeds test passwords in
`/home/appuser/.pgpass`, owned by UID 1001 with mode 0600. The PostgreSQL init
script creates the matching roles **only when its data directory is empty**.
Existing volumes are preserved; rebuilding does not recreate database users.
Public API, internal probes, and PostgreSQL host mappings are loopback-only.

For an optional custom Cedar replacement, use the read-only mount instructions
in [configuration](configuration.md#standalone-cedar-admission-required).
The file replaces the built-in policy completely. Keep the same final config,
policy bytes, source paths and DB target during existing-data review.

## Compose: new, entirely empty database

Only when this deployment's PostgreSQL catalog is genuinely new and entirely
empty, use the supplied explicit initialization override:

```sh
docker compose -f docker-compose.yaml -f docker-compose.initialize-empty.yaml config --quiet
docker compose -f docker-compose.yaml -f docker-compose.initialize-empty.yaml up --build -d
```

This adds `serve --initialize-empty`; it neither deletes data nor approves an
existing catalog. It is safe on same-target restarts after consumption. The base
Compose command and Task lifecycle commands deliberately do not add it.

## Compose: existing database

For a catalog already running the current runtime with a consumed same-target
receipt and clean schema 30, preserve its final config and credentials and start
normally:

```sh
docker compose config --quiet
docker compose up --build -d
```

For a historical catalog, **do not start the new server yet**:

1. Stop and drain all readers, writers, sync workers, controllers and embedding
   applications; prevent automatic restarts. `docker compose stop registry-api`
   stops only this deployment, not other users of the database.
2. Take and verify a full database, roles, config and policy backup; see
   [database recovery](database.md#backup-and-recovery).
3. Complete the [offline exposure procedure](registry-exposure-migration.md#maintenance-procedure)
   with the current binary and the exact final config. Stop at schema 29,
   inventory, explicitly acknowledge whole-catalog exposure, retire claims,
   finalize offline, then migrate forward to 30. No old/new writer overlap or
   generic rolling migration job is supported.
4. Start base Compose as above, without the initialization override for first
   existing-data receipt consumption. Never point an old binary at schema 30.

To run the guide's offline CLI commands in the Compose network, first build the
image and ensure PostgreSQL is available without starting the API:

```sh
# Base Compose: explicitly choose the same CONFIG_FILE as serving (default shown).
export CONFIG_FILE=config-docker.yaml
REGISTRY_CONFIG_PATH="/examples/${CONFIG_FILE}"
docker compose build registry-api
docker compose up -d postgres
docker compose run --rm --no-deps registry-api \
  migrate up --config "${REGISTRY_CONFIG_PATH}" --to-version 29 --yes
```

Replace the `migrate up ...` arguments with each reviewed offline command from
that guide, always passing `--config "${REGISTRY_CONFIG_PATH}"` to inventory,
acknowledgement, retirement, finalization and migration. This shell variable is
an explicit **container path**, not an automatically resolved CLI setting.
`CONFIG_FILE` defaults to `config-docker.yaml` and selects a file under `/examples`
only in base Compose's serving command. Replace the exported filename above with
your selected file; if you selected it in Compose's `.env`, copy that value here
explicitly (`.env` does not populate your shell). Keep this exported selection for
all maintenance and serving commands.

`run` retains the service's config/data mounts and pgpass environment, but replaces
its `command` and does not publish service ports. For a custom override mounting
configuration at `/etc/registry/config.yaml` and setting the serving command to
use that path, use this variant for maintenance:

```sh
REGISTRY_CONFIG_PATH=/etc/registry/config.yaml
docker compose -f docker-compose.yaml -f operator.override.yaml build registry-api
docker compose -f docker-compose.yaml -f operator.override.yaml up -d postgres
docker compose -f docker-compose.yaml -f operator.override.yaml run --rm --no-deps registry-api \
  migrate up --config "${REGISTRY_CONFIG_PATH}" --to-version 29 --yes
```

Use your actual override filename and mounted path. Pass those same `-f` files and
explicit `--config` path to **every** offline command, including inventory and
finalization, and use the same overrides when serving. An override's serving
`command` does not select configuration for `run` when CLI arguments replace it.
Keep policy/environment overrides and the database host spelling unchanged between
review and serving. Do not run these migration commands on a live catalog.

## Verify and manage Compose

Obtain a token for the configured issuer/audience. Store the line
`Authorization: Bearer <access-token>` in a private mode-0600 file outside the
repository; set `AUTH_HEADER_FILE` to its absolute path. No token is required for
internal probes.

```sh
docker compose ps
docker compose logs registry-api
curl --fail http://localhost:8081/health
curl --fail http://localhost:8081/readiness
curl --fail-with-body --header "@${AUTH_HEADER_FILE:?Set a private header file}" \
  http://localhost:8080/registry/default/v0.1/servers
```

`/health`, `/readiness`, `/version`, and enabled `/metrics` exist **only on 8081**.
Keep that unauthenticated listener off public proxies. See
[observability](observability.md) for telemetry configuration.

Normal lifecycle commands preserve named volumes:

```sh
task docker-up           # compose up --build, attached
task docker-up-detached  # compose up --build -d
task docker-logs
task docker-down         # compose down, no volume removal
```

These Task commands use base Compose only. For a new install use the explicit
override above; for a custom override continue to use its `docker compose -f ...`
commands. To stop without removing containers, use `docker compose stop`.
Use a separate new catalog when you need an empty environment. Never remove
volumes as routine upgrade or troubleshooting hygiene.

Changing source/config or frozen policy files requires restarting the API; changing
a source file's contents is detected on its configured polling interval. Leased
snapshots and sync state live in PostgreSQL, not an independently disposable cache.

## Images and standalone containers

Published images use **`ghcr.io/stacklok/thv-registry-api`**. Select a tag/digest
verified to contain the current catalog runtime; do not assume a published `latest`
contains this branch. For a source build with an explicit local tag:

```sh
docker build --platform linux/amd64 -t thv-registry-api:local .
```

This root Dockerfile hardcodes `GOARCH=amd64`: build and run it as **linux/amd64**,
using emulation if your host is not AMD64. Deploy this image only to compatible
Kubernetes nodes (`kubernetes.io/arch: amd64`), not native ARM64 nodes. It runs as
UID 1001. In contrast, `task build-image` runs
`ko build --local -B` with default `REPO=ghcr.io/stacklok/toolhive-registry-server`;
use the image reference ko actually prints. It does not automatically create
`thv-registry-api:local` or a published release tag. Compose uses its own
[development Dockerfile](../docker/dockerfile-compose), which sets `GOOS=linux`
but does not hardcode `GOARCH`; it builds for the selected builder platform.

A standalone container needs an already provisioned PostgreSQL endpoint, final
config with paths valid inside the container, and credentials. Use a pgpass file
with entries matching **that config's** host, port, database and both users.
Prepare a deployment-only copy owned by UID 1001, mode 0600, in a traversable
host directory; do not mount a user's private file under `/root`. With rootless
engines use the engine's UID mapping when preparing ownership. Verify readability
as the actual container user, never broaden pgpass permissions to fix access.

```sh
docker run --platform linux/amd64 -d --name registry-api \
  -p 127.0.0.1:8080:8080 -p 127.0.0.1:8081:8081 \
  --mount "type=bind,src=${REGISTRY_CONFIG_DIR:?Set absolute config directory},dst=/config,readonly" \
  --mount "type=bind,src=${PGPASS_PATH:?Set prepared absolute pgpass path},dst=/run/pgpass,readonly" \
  -e PGPASSFILE=/run/pgpass \
  thv-registry-api:local serve --config /config/config.yaml
```

Add `--initialize-empty` only for a new empty catalog; otherwise perform the offline
upgrade first. Source files, optional policy, CA certificates and secret files must
also be mounted at the absolute paths configured. The production image need not
contain curl or a shell; probe from the host or orchestration layer.

## Credentials and production operation

The supplied Compose passwords and baked pgpass are for development only. Editing
`docker/pgpass` does not change the image's embedded file; changing
`POSTGRES_PASSWORD` or the init script does not rotate roles in an existing volume.
Do not recreate/delete a database to rotate passwords. Use an administrative DB
connection to change the real roles, then securely replace mounted consumer
credentials and restart consumers as described in
[database password rotation](database.md#password-rotation).

For production, use separate limited runtime and schema-owning migration users,
external read-only credentials rather than this development image, PostgreSQL TLS,
verified backups and trusted TLS ingress on **8080 only**. Do not publish PostgreSQL
or 8081 externally. [Database setup](database.md#setup-guide) and
[authentication](authentication.md) describe the required inputs.

## Troubleshooting

- **Startup refused:** inspect logs for invalid final config, missing exposure
  evidence or dirty schema. Remain offline for migration recovery; never force a
  schema version or use empty initialization to bypass review.
- **DB authentication failed:** check the mounted file's ownership/mode and exact
  host/database/user tuple. Do not dump environment variables or credential files.
- **401/403:** verify issuer/audience and token expiry, then Cedar permission. A
  healthy internal probe does not prove consumer admission.
- **Not ready:** check database and initial operator snapshot failures. A failed
  refresh preserves the last good snapshot, not an empty successful catalog.
