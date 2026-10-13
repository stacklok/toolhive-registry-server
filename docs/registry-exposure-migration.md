# Offline exposure review and explicit claims retirement

This guide documents offline review and explicit archive-and-clear commands.
`serve` now selects [`internal/app.NewCatalogApp`](registry-standalone-assembly.md):
final config/policy validation precedes migrations, then read-only compatibility
preflight and schema-29 receipt consumption precede reconciliation, repair, both
listeners and leased writers. No schema migration, `--yes`, empty/unlabeled data,
old authorization flag or builder call grants existing-data consent. Legacy code
and physical columns remain unselected until #915; do not restart old binaries
against a retired catalog.

## New, entirely empty installs only

Prepare the **final** config first, with trusted OAuth providers, administration off
unless explicitly needed, no retired fields, and a readable operator-authored Cedar
policy mounted read-only. A deny-all policy is valid but grants no access. Then,
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
install, alongside `policy.existingConfigMap`, trusted OAuth and DB settings. The
chart also requires an explicit `image.registryServerUrl` built from this startup
change; it has no legacy-image fallback. Both ct fixtures opt in only for their disposable
test DB and use externally mounted deny-all policy. Production images need no shell/jq.
Compose: set `REGISTRY_POLICY_FILE` to the absolute operator-authored policy path,
configure the OAuth provider in the selected config, and only for an empty install use
`docker compose -f docker-compose.yaml -f docker-compose.initialize-empty.yaml up --build`.
Base Compose does not initialize implicitly; neither command removes existing volumes.

## Maintenance procedure

1. Stop and drain **all** registry readers, writers, sync workers and Kubernetes
   controllers, including old binaries and embedding applications. Prevent restarts
   and background reconciliation. Take and verify a database/config/policy backup.
   A digest or absence of a live lease cannot prove old processes are offline.
2. Apply schema migrations separately, with the existing migration configuration:

   ```sh
   thv-registry-api migrate up --config /etc/registry/config.yaml --yes
   ```

   Migration 000027 creates only a stable logical database UUID and an empty
   acknowledgement row. Migration 000028 adds empty internal archive and transition
   state. Migration 000029 adds a nullable consumed-target digest and enforces that
   it and `consumed_at` are either both NULL or both populated with a valid digest.
   It refuses unsupported pre-existing consumption without target evidence; it
   never manufactures or backfills consent. None grants consent, clears claims
   or changes catalog data/generations.
   `migrate up` retains its ordinary configuration validation; exposure inspection
   separately permits inspecting retired security fields.
3. Prepare the **FINAL deployment config and policy first**, removing retired fields
   (`auth.authz`, `auth.roles`, `auth.publicPaths`, and source/view `claims`, including
   explicit nulls and environment overrides). Do not remove them from the review
   fingerprint or reinterpret an old acknowledgement. The historical database still
   has its old claims for inventory. Use this same final effective configuration,
   including environment overrides and mounted policy contents, for inventory,
   acknowledgement, retirement and eventual cutover startup. Resolve legacy operator
   annotations before that future startup; this command does not contact a cluster.

   Materialize any exposure-relevant `serve` CLI overrides in YAML/environment for
   review; offline commands load `--config` and environment only. Startup revalidates
   final overrides and freezes Cedar once for evaluation and the receipt fingerprint. Inventory
   and acknowledgement support clean schemas 27, 28 and 29; retirement requires
   28 or 29.
   They never auto-migrate, initialize sources, repair latest pointers, fetch sources
   or start controllers:

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
   or retirement command. It can be consumed by the new `serve` after preflight.

   The historical retirement command still reports **"metadata retired; runtime cutover not
   yet available in this slice"** for compatibility. With this binary, after inspecting
   successful receipt delivery, cut over using `serve --config /etc/registry/config.yaml`
   (without `--initialize-empty`). Never restart legacy binaries or embeddings. If JSON receipt
   delivery fails after the command reports **"claim metadata retirement committed;
   remain offline; inspect stored receipt"**, the database transaction already
   committed: remain offline and use the recovery inspection below rather than retrying.

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

`internal/db.ConsumeExposure` requires clean schema 29. It is not a separate
command or permission to bypass preparation. The default `app.NewCatalogApp`
composition calls it only after validating final effective config, rejecting
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
Offline fingerprints retain format 1. Changes before policy load invalidate pending
review; changes after load cannot silently bind consent to a different evaluator.

## Rollback

Before consumption, 000029 can be downgraded to 000028 without changing pending
acknowledgement or retirement evidence. It refuses to discard any consumed timestamp
or target digest. Before retirement/consumption, 000028 can be downgraded only when
its archive is empty, preserving pending acknowledgement evidence in 000027. It **refuses** to
lose any retirement, consumption or archive data. The unchanged 000027 downgrade
still refuses to discard any acknowledgement, even invalidated state. Before any
acknowledgement, downgrading/reapplying 000027 creates a new database identity and
requires fresh review. Do not delete evidence to bypass these refusals.

After clearing, restore a verified complete backup **offline** for rollback; an old
binary cannot safely recreate the retired metadata. Preserve migration state and
prevent old and leased workers from co-owning sources. No user database or cluster
is migrated by the tests; they use disposable PostgreSQL fixtures only.
