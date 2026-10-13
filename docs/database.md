# Database configuration and operation

The standalone server requires PostgreSQL 16+ for definitions, payloads, latest
pointers, sync state, leases and exposure receipts. Named views are not databases
or privacy boundaries. Deployment workflows are in
[Compose](deployment-docker.md) and [Helm/Kubernetes](deployment-kubernetes.md).

## Configuration fields

| Field | Default | Purpose |
|---|---|---|
| `host`, `port`, `database`, `user` | Required | Connection target and limited runtime login |
| `migrationUser` | `user` | Schema-owning migration login; use a separate role |
| `password` | Unset | Runtime password, alternative to pgpass |
| `migrationPassword` | `password` only when migration/runtime user match | Migration password, alternative to pgpass |
| `sslMode` | `require` | Use `verify-full` with a trusted CA and hostname-valid certificate in production |
| `maxOpenConns` | `25` | Connection pool limit per process |
| `maxIdleConns` | `5` | Idle pool size |
| `connMaxLifetime` | `5m` | Connection reuse lifetime |

Do not place passwords in committed YAML or the chart's `config` values (rendered
to a ConfigMap). `dynamicAuth` (AWS RDS IAM) and configured static passwords are
mutually exclusive. If configured, static passwords take precedence over pgpass;
`THV_REGISTRY_DATABASE_PASSWORD`/`THV_REGISTRY_DATABASE_MIGRATIONPASSWORD` are
available escape hatches, not the preferred secret interface. Provision suitable
IAM permissions and DB grants separately when choosing dynamic authentication.

## Password security

### pgpass file

Prepare a private deployment-owned file outside the repository with one entry per
login. The shape is:

```text
hostname:port:database:username:password
```

For [config-database-dev.yaml](../examples/config-database-dev.yaml), the two tuples
are `localhost:5432:toolhive_registry:thv_user` and
`localhost:5432:toolhive_registry:thv_migrator`; supply their real passwords through
your secret manager or a private editor, not shell command arguments/history.
Escape `:` and `\` in fields per the
[PostgreSQL pgpass rules](https://www.postgresql.org/docs/current/libpq-pgpass.html).
Use the host spelling configured by the client: a file for `postgres-service` does
not match a config pointing at `postgres`.

Set mode **0600** and ownership to the UID running the client; parent directories
must be traversable by that UID. Set `PGPASSFILE` to its absolute path or use the
client's `~/.pgpass`. Never make it group/other-readable. In containers, a root-owned
0600 Secret or `/root/.pgpass` is not readable by the application. Follow the
[UID-1001 container pattern](deployment-docker.md#images-and-standalone-containers)
or [UID-65535 init-copy pattern](deployment-kubernetes.md#prepare-operator-valuesyaml)
instead of widening final pgpass permissions.

```sh
chmod 600 "$PGPASSFILE"
thv-registry-api serve --config /etc/registry/config.yaml
```

The serving command assumes a prepared current catalog; new empty initialization
and existing-data migration differ as described below.

### Password rotation

Changing a credential file, Helm Secret or `POSTGRES_PASSWORD` does **not** change
an existing PostgreSQL role's password. Through an authorized administrative
connection, use interactive psql `\password thv_user` and
`\password thv_migrator` (substitute your actual role names). This changes the roles
without putting password literals in SQL history. Arrange consumer downtime or
an explicitly planned credential cutover; existing sessions may persist while new
connections fail with old credentials.

Securely update the matching pgpass entries, mount/reload the new file and restart
consumers. For Helm's prepared emptyDir file, recreate pods after updating the
Secret; for a single-file bind mount, recreate the container if the secret was
replaced atomically on the host. Check new connections succeed without logging
passwords. `prime-db` does not rotate an existing user's password. The development
Compose image embeds fixed test credentials and is not a production rotation
mechanism.

## Setup guide

Provision a database and two distinct logins before serving:

- `thv_user`: non-superuser, member of `toolhive_registry_server`; normal catalog
  DML, sequence use and schema usage, not schema ownership/DDL.
- `thv_migrator`: non-superuser, member of that role and owner of the catalog schema
  objects; requires schema CREATE and database access for migrations. Existing
  installations must preserve/arrange ownership of existing objects, not simply
  grant CREATE to a new login.

The migration SQL grants current/default table and sequence privileges to the
`toolhive_registry_server` role. Do not use a PostgreSQL superuser as runtime
`database.user`. Runtime is not read-only: reconciliation and leases require writes.

### Bootstrap a new development database

Connect interactively as an authorized DB administrator with psql, targeting a
maintenance database (using a private pgpass or an interactive password prompt).
For a **new** database/roles only:

```sql
CREATE ROLE toolhive_registry_server NOLOGIN;
CREATE ROLE thv_user LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
CREATE ROLE thv_migrator LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
\password thv_user
\password thv_migrator
CREATE DATABASE toolhive_registry OWNER thv_migrator;
\connect toolhive_registry
GRANT CONNECT ON DATABASE toolhive_registry TO thv_user;
GRANT USAGE ON SCHEMA public TO thv_user;
GRANT USAGE, CREATE ON SCHEMA public TO thv_migrator;
GRANT toolhive_registry_server TO thv_user, thv_migrator;
```

Database/schema ownership supplies migration DDL privileges without making either
login a superuser. On managed PostgreSQL, ask the provider administrator to supply
equivalent ownership/grants. Do not execute CREATE commands blindly against an
existing deployment.

Prepare pgpass for both roles; customize trusted OAuth in the selected config.
If changing its issuer, update/remove the corresponding `identityMappings` entry;
the built-in policy needs no group/scopes. Keep admin off. Then use the new-empty
or existing-data path below. The root Compose setup provisions different role names
(`db_app`/`db_migrator`) and database (`registry`) with development-only passwords;
do not combine its credentials with the `thv_*` example unchanged.

### Using prime-db

Alternatively, `prime-db` creates the shared role and a login if missing, then grants
role membership. It requires the positional **USER** and an administrative connection
configured through `database.migrationUser` (or `user` if omitted). Prepare a
separate bootstrap config with that administrator and its private connection
credentials; do not turn the final runtime login into an administrator.

```sh
thv-registry-api prime-db thv_user --config /private/bootstrap.yaml
thv-registry-api prime-db thv_migrator --config /private/bootstrap.yaml
```

Each command reads the new user's password from a hidden terminal prompt, or stdin
redirected from a protected secret file. It does **not** create the database, grant
schema ownership/CREATE/USAGE/CONNECT, or change existing passwords. Apply the
remaining database/schema grants from the bootstrap SQL above as administrator.
`--dry-run` prints password-bearing SQL: do not use it with real credentials or
capture its output in logs. A role grant alone is not a complete migration setup.

## Database migrations

### New empty catalog

With final config, trusted authentication and a virgin entirely empty catalog:

```sh
thv-registry-api serve --config /etc/registry/config.yaml --initialize-empty
```

Startup validates final config and frozen Cedar, applies guarded forward schema
migrations, performs read-only source/operator preflight, consumes empty-install
evidence, then reconciles definitions and serves at clean schema 30. The flag is
explicit consent for **empty installation only** and never approves existing data.
It may remain on same-target restarts without resetting receipts.

### Existing catalog

Do not treat automatic startup migrations or `migrate up --yes` as an upgrade
plan. Stop/drain all readers, writers, controllers and embedding applications and
prevent restarts; take/verify full backups first. Use the final binary/config and
the [offline exposure procedure](registry-exposure-migration.md#maintenance-procedure):

1. `migrate up --to-version 29` stops at the historical review schema.
2. Inventory and explicitly acknowledge whole-catalog exposure, then retire claims.
3. `migrate exposure finalize --confirm-offline` consumes evidence **at 29**.
4. Only then migrate forward to 30 and start the new runtime.

Follow the linked procedure for exact digest-bound commands, pending-receipt resume
rules and operator preflight; do not improvise consent or re-acknowledge retired
state. Even unlabeled or currently unlinked data is existing data. Old and new
writers must never overlap during this transition. Default forward migration may
stop cleanly at 29 awaiting finalization; it does not always bring a catalog to the
latest schema automatically.

For a current-runtime DB with clean schema 30 and a consumed same-target receipt,
normal restarts need no new exposure acknowledgement. Assess future schema/image
compatibility before upgrades. **Schema 30 has no supported downgrade.**
`migrate down --num-steps ...`, a Task rollback or Helm rollback cannot undo claims
retirement or reconstruct dropped columns; do not restart old binaries there.

### Backup and recovery

Before offline cutover, take a consistent full PostgreSQL backup plus global
roles/grants, final config, secret provisioning and policy files; verify restoration
on an isolated target. Use your managed backup service or authorized `pg_dump` /
`pg_dumpall --globals-only` connections with file-based credentials. Protect backup
files: they contain catalog payloads and potentially sensitive migration archives
and role hashes. Archives/receipts are evidence, **not** a complete backup.

For recovery, remain offline, restore a verified full pre-cutover backup and its
matching config/policy/roles into an operator-controlled target, and review target
continuity before serving. Restoring to another DB target is not permission to
silently rebind a receipt. On uncertain commit responses or dirty schema state,
inspect evidence using the offline guide; never auto-force migration versions,
clear receipts, delete data or retry retirement blindly.

## Connection pooling and monitoring

Budget `maxOpenConns` across replicas plus migration/offline connections. Tune
against PostgreSQL's actual capacity and request/ingestion load; a larger pool is
not automatically faster. `connMaxLifetime` limits connection reuse, not query time.
Use internal 8081 probes and [telemetry](observability.md) to monitor readiness,
HTTP errors and complete leased sync attempts. Keep PostgreSQL and internal
operational listeners off the public network.
