# Offline exposure inventory and pending acknowledgement

Improves on #912. This increment implements review tooling and a **dormant**
revision-bound marker, not claims/RBAC retirement or a cutover. Cedar admission
remains additive to all legacy protections. No claims are cleared or archived.
The public persistence contracts and their legacy mutation guards are unchanged.

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
   acknowledgement row. It grants no exposure consent and changes no catalog data
   or source generations. `migrate up` retains its ordinary configuration validation;
   exposure inspection separately permits inspecting retired security fields.
3. Use the **effective deployment configuration**, including its environment
   overrides and mounted policy file, for all exposure commands. Materialize any
   exposure-relevant `serve` CLI overrides in YAML/environment for this review;
   these offline commands load `--config` and environment only. The future cutover
   preflight must also revalidate the final startup overrides. These commands
   require clean schema version 27 and never auto-migrate, initialize sources,
   repair latest pointers, fetch sources or start controllers:

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
   the standalone operator compatibility gate remains future cutover work.
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
the pending review. Re-inventory and explicitly acknowledge again while offline. If you resume
the existing protected runtime, its normal reconciliation/jobs can invalidate the
marker. This slice does **not** enforce the marker at startup. A later slice must
consume/revalidate the pending marker before retiring old guards, resolving retired
config/API fields and operator annotations first. That cutover is neither approved
nor executed here. After eventual consumption, legitimate future writes must not
be subject to perpetual historical-digest matching.

## Rollback

Before acknowledgement, migration 000027 can be downgraded without catalog changes;
reapplying it creates a new identity and requires a fresh inventory. Downgrade
**refuses** to discard an existing acknowledgement, even one invalidated by later
changes. Do not delete the marker to bypass that refusal. Preserve backup and
migration state and plan any rollback offline. This increment does not authorize
an old binary to co-own a source with leased workers or to bypass legacy guards.
