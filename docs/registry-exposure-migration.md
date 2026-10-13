# Offline exposure review and explicit claims retirement

This guide documents offline review and explicit archive-and-clear commands.
`serve` now selects [`internal/app.NewCatalogApp`](registry-standalone-assembly.md):
final config/policy validation precedes migrations and read-only compatibility
preflight. Historical evidence is finalized offline at schema 29 before forward
cleanup to schema 30; ordinary serving and every public PostgreSQL write require
clean schema 30. No schema migration, `--yes`, empty/unlabeled data, old authorization
flag or builder call grants existing-data consent. **One final binary provides all
commands below; no unreleased intermediate binary is required.** Migration 30 drops
only the live source/view/entry claims columns and their dependent objects, preserving
logical identity, historical archives and receipts. Never restart an old binary
against a retired catalog.

## New, entirely empty installs only

Prepare the **final** config first, with trusted OAuth providers, administration off
unless explicitly needed and no retired fields. Omit `authorization.policyFile` for
evaluated built-in allow-all Cedar, or mount a valid custom restriction read-only.
Every verified identity gets every enabled operation by default, including admin if
enabled; anonymous catalog access still requires explicit opt-in. A deny-all custom
policy is valid but grants no access. Neither policy grants exposure consent. Then,
only for a genuinely empty new database, explicitly authorize empty-install mode:

```sh
thv-registry-api serve --config /etc/registry/config.yaml --initialize-empty
```

The flag defaults to false. After final config/frozen-policy validation and read-only
source/operator preflight, preparation locks **every catalog table**, schema, receipt
and archive in a serializable transaction. A virgin logical identity and all-empty
catalog are required; SQL NULL claims do not make a row empty. Empty acknowledgement
and first consumption commit together, preserving the full pending validation and
excluding external writers between them. A valid pending offline empty review may
be reused without changing its evidence; changed pending config/policy/catalog/target
fails. Pending existing-data or retired decisions cannot be activated by this flag:
complete the manual chain below and omit it for first consumption.

Retaining the flag on replicas/restarts is safe once consumed: verify only the
existing target continuity, without resetting, fencing or rebinding the receipt,
even after normal catalog/config/policy edits. No policy permission is supplied.
The offline `initialize-empty` command below remains available for maintenance;
it is not a second chart/job activation framework.

Helm: `initializeEmptyCatalog: false` by default; explicitly set true for a new empty
install, alongside trusted OAuth and DB settings. `policy.existingConfigMap` is optional
and replaces the built-in policy entirely. The
chart also requires an explicit `image.registryServerUrl` built from this startup
change; it has no legacy-image fallback. Both ct fixtures opt in only for their disposable
test DB: one exercises the built-in policy and one the optional deny-all mount.
Production images need no shell/jq. Compose requires no external policy mount by
default. Configure the OAuth provider in the selected config (see configuration
reference for an optional read-only custom mount), and only for an empty install use
`docker compose -f docker-compose.yaml -f docker-compose.initialize-empty.yaml up --build`.
Base Compose does not initialize implicitly; neither command removes existing volumes.

## Maintenance procedure

1. Stop and drain **all** registry readers, writers, sync workers and Kubernetes
   controllers, including old binaries and embedding applications. Prevent restarts
   and background reconciliation. Take and verify a database/config/policy backup.
   A digest or absence of a live lease cannot prove old processes are offline.
2. Prepare the **FINAL deployment config, effective Cedar policy and operator export
   annotations first**. Omitted policy resolves to the same built-in bytes for inventory,
   acknowledgement, retirement, finalize and startup, not an `absent` sentinel. A custom
   file replaces the default entirely; explicitly configured unavailable or invalid files
   fail startup. The independent version-1 fingerprint format and explicit-file pending
   receipts remain unchanged. Remove retired fields by presence, including explicit nulls
   and environment overrides. Preserve the same effective YAML/environment, policy
   contents and database host spelling throughout review and finalization. Materialize
   exposure-relevant `serve` CLI overrides in YAML/environment for offline commands.
   Migration commands accept valid final inline source definitions; they do not use
   obsolete runtime source validation.
3. With this final binary, stop at the historical review schema explicitly:

   ```sh
   thv-registry-api migrate up --config /etc/registry/config.yaml --to-version 29 --yes
   ```

   `up` rejects a target below the existing version; it never downgrades. Default
   `up` from an older schema first reaches 29 and checks cleanup eligibility **before**
   golang-migrate marks 30 dirty. Known refusals leave clean 29 and give the offline
   procedure. `--yes` confirms schema migration only, never exposure consent.
   Schemas 27–29 introduce identity/receipt/archive bookkeeping only, without
   manufacturing approval or clearing claims. A pending retired receipt must resume
   at **finalize**, not inventory/re-acknowledgement/retirement. An already-consumed
   receipt can be verified at finalize, then upgraded.

   Use the final effective config for all review commands. They never migrate,
   initialize definitions, repair latest pointers, fetch sources or start controllers:

   ```sh
   thv-registry-api migrate exposure inventory --config /etc/registry/config.yaml
   ```

   Review the JSON counts, source/view/entry identifiers, ownership, generations,
   ordered view links, unlinked records and claims classifications. `sql-null`,
   `json-null` and `empty-object` are distinct unlabeled classes; `malformed` and
   `labeled` are also reported. **Unlabeled is not public or empty.** When legacy
   authorization was enabled, unlabeled records were default-denied too.
   The fingerprint covers every column of every catalog table, including all
   server/skill/plugin versions and payload tables, latest pointers, retained
   snapshots and sync/lease/baseline state, not only currently linked entries.
   Output never includes raw claims, payloads, policies or credentials. This is an
   offline database inventory: it includes persisted operator exports, but does not
   contact Kubernetes or upstream sources. Review live export annotations separately;
   the standalone startup gate validates live exports over the full effective scope.
4. Decide whether **all historical catalog data**, including unlinked and retained
   data, can be shared with a future whole-instance reader population. If not,
   remain on the protected runtime, or prepare separate instances/host-owned
   personalized catalogs; the tool cannot translate claims into equivalent Cedar
   instance permissions. To record that deliberate decision, replace the digest
   below with the exact reviewed inventory digest:

   ```sh
   thv-registry-api migrate exposure acknowledge \
     --config /etc/registry/config.yaml \
     --digest 'sha256:<reviewed-64-hex-digest>' \
     --approve-whole-catalog --confirm-offline
   ```

   For a genuinely fresh database, **before any config reconciliation**, use the
   alternative explicit new-install path:

   ```sh
   thv-registry-api migrate exposure initialize-empty \
     --config /etc/registry/config.yaml \
     --digest 'sha256:<reviewed-64-hex-digest>' --confirm-offline
   ```

   Every persisted catalog table must be empty, not just entries. The schema identity
   row is allowed; future sources/views in YAML are allowed. No automatic empty or
   unlabeled approval exists. `--yes` never supplies either exposure or offline consent.

5. For a historical whole-catalog acknowledgement, use its exact
   `acknowledgement_id` and **`post_fence_digest`**, not its original `reviewed_digest`:

   ```sh
   thv-registry-api migrate exposure retire-claims \
     --config /etc/registry/config.yaml \
     --acknowledgement-id '<pending-acknowledgement-uuid>' \
     --digest 'sha256:<post-fence-64-hex-digest>' \
     --approve-whole-catalog --confirm-offline
   ```

   Both consent flags are mandatory; `--yes` is not consent. The final config must
   have no retired fields. A changed config/policy requires fresh inventory and
   acknowledgement **before** clearing; arbitrary rebinding is never accepted.
   `initialize-empty` already has valid new-install evidence and needs no archive
   or retirement command. The historical retirement JSON message remains unchanged
   for compatibility; it is **not** permission to serve. After successful retirement
   remain offline and continue:

   ```sh
   thv-registry-api migrate exposure finalize \
     --config /etc/registry/config.yaml --confirm-offline
   thv-registry-api migrate up --config /etc/registry/config.yaml --yes
   thv-registry-api serve --config /etc/registry/config.yaml
   ```

   Finalize reuses mandatory preparation: final config/retired-presence rejection,
   one frozen Cedar load, read-only historical sources/all views and full-scope
   operator compatibility preflight, then serialized consumption at 29. It **stops
   before** CONFIG reconciliation, latest repair, ingestion, workers or listeners.
   `--confirm-offline` is mandatory; consent comes from the existing exact pending
   acknowledgement/retirement, not `--yes` or a new approval shortcut. Empty pending
   mode must still have zero rows in every catalog table. A consumed same-target
   receipt is verified without rewriting any historical fields.

   Migration 30 refuses any live non-SQL-NULL claims (including JSON null and `{}`),
   unconsumed review/retirement, nonempty unapproved catalog, any archive evidence
   without consumed state, and every DB-time live lease, including stale generations.
   A virgin entirely empty identity can undergo **structural cleanup only**, with no
   acknowledgement: fresh installs still require explicit `--initialize-empty`.
   Archives are not a full backup. Keep backups and sensitive archives restricted.
   On lost commit responses or output failure, remain offline and inspect stored
   receipts; never assume rollback, re-acknowledge or silently rebind evidence.

   The runner serializes its entire staged workflow across final-binary replicas;
   per-step migration locks alone would let another pod reach 30 before a delayed
   `Migrate(29)` and inadvertently attempt a downgrade. Direct raw migrators do not
   participate in this runner lock: no raw migration or other database writer may
   run concurrently with offline upgrade. Preflight and the SQL guard are separate
   transactions. A race/direct raw migrator
   can still mark 30 dirty before its authoritative locked SQL guard refuses and
   rolls back DDL. Do not auto-force a version or claim that this race stays clean:
   remain offline, close the raw migrator/connection to release its aborted
   transaction and session lock, then independently inspect schema/receipt state
   and restore a verified backup or perform explicitly reviewed database recovery. Known
   cleanup eligibility refusals at 29 and the supported schema-30 `migrate down`
   refusal happen before the dirty mark; historical down guards do not share that guarantee.

   The owned migration connection computes a fresh host/port/current-database/logical-ID
   fingerprint under the receipt lock and installs it as transaction-local verification
   context. SQL compares it to the consumed receipt before DDL and fails closed if it
   is absent. No stored digest
   is copied into that context or rebound. Virgin structural cleanup needs no receipt
   or target context. Runner cancellation reaches acquisition, version reads and DDL.
   The executing migration backend itself holds the whole-workflow session lock until
   it exits, so closing or losing the caller/control session cannot unlock a still-running
   migration. Queries are joined, the owned connection is closed with bounded cleanup,
   and server exit is observed using the original PID and backend start time captured
   while the owned connection was live. Cleanup never signals a numeric PID: a reused
   PID belongs to a different session. Repeated close returns the cached cleanup result.
   An unconfirmed cleanup error means the original backend may still hold the workflow
   lock until server exit; it requires offline inspection and is not permission to resume
   migration or serving. Cancellation during schema-30 cleanup rolls back its DDL and
   preserves archives/receipts, but may leave version 30 dirty from the prior dirty mark;
   recover offline as described above, never auto-force the version.

   The migration driver's `Drop` operation intentionally refuses whole-database
   destruction to protect catalog data and historical evidence. It is not an upgrade
   or recovery mechanism; use a verified offline backup and reviewed recovery instead.

## What recording does (and does not do)

Acknowledgement runs in a serializable transaction with write-excluding catalog
locks, recomputes the reviewed digest and rejects a mismatch. It rejects **every**
DB-time unexpired lease, including stale-generation leases, without stealing or
clearing them. After offline review it advances every source generation and sets
only `registry_sync.applied_generation` to NULL. Payloads, claims, IDs, ownership,
view ordering, latest pointers, hashes, counts, lease tokens and last-success
state remain intact. Expired pre-review jobs cannot commit or complete at the new
generation. Any failure rolls back both fences and the marker.

The row atomically records acknowledgement UUID/mode, reviewed digest, post-fence
catalog digest, config revision and DB-time timestamp. Output says exactly:
**"acknowledgement recorded; legacy protections unchanged"**. It is pending review
state, not authorization to expose data and not proof of an offline drain.

The versioned digest binds stable logical DB identity and connection target,
persisted catalog state, effective exposure-relevant source/view/auth/admin/operator
configuration, retired-field presence (including explicit YAML nulls), relevant
environment overrides and policy **contents**, not only its path. Policy files must
be bounded readable regular files; inspection does not validate/evaluate Cedar or
assert that a cutover configuration is deployable. Credentials are not emitted and
need not invalidate a review. Clock ticks, connection IDs and session formatting
are not revisions; actual catalog/lease writes are.

Any subsequent catalog, target, exposure-relevant config or policy change invalidates
pending review. Re-inventory and explicitly acknowledge again while offline, before
retirement. Resuming the protected runtime before retirement can invalidate the
marker. Startup enforces and consumes valid pending evidence through the full
preparation barrier. Legitimate writes after consumption do not undergo perpetual
historical-digest matching.

## Explicit retirement and recovery evidence

Retirement requires the existing pending whole-catalog acknowledgement, unchanged
config/policy revision and current post-fence database digest. It uses a serializable
transaction with the same write-excluding catalog locks, also locking the archive.
Every DB-time unexpired lease (including stale generations) is rejected without
stealing tokens. All source/view/entry rows are archived, even SQL NULL, JSON null,
empty-object and malformed claims. The archive retains raw JSONB, its SQL NULL
discriminator, original row UUID/name, source UUID/name, kind and ownership, keyed
by a new retirement operation UUID. JSONB preserves stored values, not original
pre-JSONB whitespace/key order. No catalog payloads are changed or copied to the
archive; recover the complete catalog from the verified backup.

Then only the three live claims columns are set to **SQL NULL**, every source
generation advances once, and applied generations are invalidated. IDs, ownership,
payload extras, kind-specific latest pointers, view order, counts, last-success
state, hashes and lease tokens remain intact. Archive, clearing, fencing and the
retirement receipt are atomic within a transaction that commits successfully: any
pre-commit error rolls all of them back. A lost commit response has an uncertain
outcome, and a JSON receipt-output failure occurs after a successful commit; neither
is evidence that rollback occurred. The original reviewed digest, post-fence digest,
acknowledgement ID/config/time are never changed by retirement. A new post-retirement
digest and DB-time retirement timestamp are recorded separately. `consumed_at` and
`consumed_target_digest` remain NULL; retirement never consumes the receipt.

The archive is sensitive internal migration evidence, with no live-catalog foreign
keys/cascades, consumer API, or catalog query path. Restrict database/backup access
and retain it with the backup. Canonical inventory format 1 continues fingerprinting
the explicit live-catalog tables; transition bookkeeping and archive contents are
excluded, never printed. The immutable original reviewed digest fingerprints the
original decision evidence, including raw claims and SQL NULL distinctions. Schema
27 → 28 → 29 alone therefore does not invalidate an existing review. Application tooling
never edits archive rows or re-acknowledges retired/consumed state.

**Retries:** repeats, wrong IDs and attempts to use the post-retirement digest as a
new acknowledgement are rejected without mutation. If the response is lost or the
command reports an uncertain outcome, remain offline and inspect the stored receipt;
do not delete state, retry retirement or re-acknowledge it. An authorized database
operator can run this read-only query, which exposes no raw claims:

```sql
SELECT acknowledgement_id, retirement_id, acknowledged_at, retired_at,
       reviewed_digest, post_fence_digest, post_retirement_digest
FROM public.exposure_acknowledgement
WHERE singleton;
```

A pending acknowledgement has a non-NULL `acknowledgement_id` and NULL
`retirement_id`/`retired_at`; a committed retirement has non-NULL
`retirement_id`, `retired_at` and `post_retirement_digest`. Treat any other result
or an unavailable query as unresolved and remain offline while investigating. A
rolled-back attempt may be retried only when its rollback is independently
established and the same pending evidence still matches; otherwise inventory and
acknowledge again before retirement. After retirement, changes invalidate pending
cutover evidence and cannot be silently rebound; retain downtime and restore the
backup offline if necessary. There is no restore or automatic rebinding command.

## Receipt consumption and target continuity

`internal/db.ConsumeExposure` supports clean schemas 29 and 30. The explicit offline
`migrate exposure finalize` command reuses the same preparation barrier as
`app.NewCatalogApp`, stopping immediately after consumption/target verification.
Normal serving uses schema 30; historical pending format-1 decisions must be
finalized while schema 29 still has its live claims columns. The default composition
calls consumption only after validating final effective config, rejecting
retired-field presence, freezing the actual loaded Cedar engine and completing
read-only compatibility preflight over all final configured and persisted sources
and operator scopes. See the [assembly guide](registry-standalone-assembly.md).
The CLI selects this composition; public persistence stays policy-free.

The primitive locks the explicit catalog/receipt/schema tables in EXCLUSIVE mode
inside a serializable transaction before reading the snapshot. On first consumption
it requires the exact pending config/policy revision and current post-retirement
digest for a whole-catalog decision; a whole-catalog acknowledgement alone is
insufficient, even for unlabeled rows. Empty-mode consumption requires the current
post-fence digest and **every** catalog table still empty. Every DB-time unexpired
lease, including stale-generation leases, blocks consumption. It advances source
generations and invalidates only applied baselines, preserving payloads, counts,
last-success state, hashes and tokens. It atomically stores a DB-time `consumed_at`
and target digest without modifying any pending acknowledgement/retirement fields
or archive. Failures before commit roll back; concurrent starts serialize, with
bounded serialization/deadlock retries. A lost commit response requires receipt
inspection, not an assumption of rollback.

The consumed-target fingerprint is SHA-256 of the UTF-8 JSON encoding of this
unambiguous, versioned tuple (no trailing newline):

```text
["registry-exposure-consumed-target", 1, logicalDatabaseUUID,
 connectionConfigHost, connectionConfigPort, current_database()]
```

It uses the configured host spelling, **not** a DNS-resolved address, credentials,
config, policy or catalog. Same-target restarts verify only this digest; legitimate
catalog/config/policy changes do not rebind or invalidate historical evidence.
They still need normal current config, Cedar and source/operator compatibility
validation by the preparation barrier. Host aliases are a target change,
consistent with inventory semantics, and fail closed without silent rebinding.

A consumed backup restored to the **same target** retains continuity. On another
target, use the correct reviewed endpoint, or restore an **offline pre-retirement
backup** and perform a new explicit review/retirement for that target. There is no
relocation/rebinding tool. A cloned database behind the identical endpoint and
logical UUID is indistinguishable: this digest is continuity evidence, **not** an
attestation of physical database uniqueness.

Inspect consumption without exposing archived claims:

```sql
SELECT acknowledgement_id, retirement_id, consumed_at, consumed_target_digest
FROM public.exposure_acknowledgement
WHERE singleton;
```

The config loader retains retired-field and environment fingerprint inputs from
the same raw YAML/environment snapshot used to decode the effective config.
`Config.ExposureReview(engine.Revision())` uses final in-memory CLI overrides and
the exact bytes frozen in the loaded Cedar engine, never rereading either file.
Offline config fingerprints retain format 1. Catalog inventory remains byte-compatible
format 1 at schemas 27–29. Schema 30 uses claims-free catalog format 2 for new empty
reviews; historical consumed format-1 receipts are preserved verbatim, never relabeled.
Pending format-1 evidence cannot be activated or rebound at 30. Changes before policy
load invalidate pending
review; changes after load cannot silently bind consent to a different evaluator.

## Rollback

Schema 30 downgrade is always unsupported: `migrate down --config
/etc/registry/config.yaml --num-steps 1 --yes` refuses before marking a version dirty.
The raw down SQL guard also refuses, but a raw migrator can already have marked the
previous version dirty. Neither command reconstructs claims from NULL/archive data.
Keep all processes offline and restore the complete verified pre-retirement backup.
For example, with operator-owned libpq service definitions targeting the stopped
source and a **separate empty restore database** (never an online catalog):

```sh
pg_dump --dbname='service=registry-offline' --format=custom \
  --file=/backups/registry-pre-retirement.dump
pg_restore --list /backups/registry-pre-retirement.dump
# Verify an actual disposable restore before approving retirement.
pg_restore --dbname='service=registry-offline-restore' --exit-on-error \
  /backups/registry-pre-retirement.dump
```

Restore config/policy/backups together; the archive contains claims evidence, not
all payloads or a full backup. A different host spelling/database endpoint needs a
pre-retirement restore and fresh explicit review, never a receipt rebind.

Before consumption, 000029 can be downgraded to 000028 without changing pending
acknowledgement or retirement evidence. It refuses to discard any consumed timestamp
or target digest. Before retirement/consumption, 000028 can be downgraded only when
its archive is empty, preserving pending acknowledgement evidence in 000027. It **refuses** to
lose any retirement, consumption or archive data. The unchanged 000027 downgrade
still refuses to discard any acknowledgement, even invalidated state. Before any
acknowledgement, downgrading/reapplying 000027 creates a new database identity and
requires fresh review. These historical/raw guard refusals may mark the attempted
version dirty; only the supported runner's known schema-30 downgrade refusal is
guaranteed clean. Do not delete evidence to bypass these refusals.

After clearing, restore a verified complete backup **offline** for rollback; an old
binary cannot safely recreate the retired metadata. Preserve migration state and
prevent old and leased workers from co-owning sources. No user database or cluster
is migrated by the tests; they use disposable PostgreSQL fixtures only.
