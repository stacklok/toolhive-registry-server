# Registry ingestion and sync

Standalone uses the public [leased reconciliation pipeline](registry-ingestion.md)
and [persistence jobs](registry-persistence-jobs.md), supervised by
`internal/sync/standalone`. The old manager/detectors/state/coordinator and unleased
Store facade are removed. See [standalone assembly](registry-standalone-assembly.md)
for preparation, readiness, ownership and shutdown.

## Source ownership and scheduling

- Scheduled Git/API/file sources are claimed through `Jobs.ClaimNext` with their
  configured interval and filter. Git supports branch/tag/commit and explicit
  password-file loading; upstream APIs follow bounded pagination; files may be a
  path or URL. Imports must validate completely before returning any snapshot.
- Inline file definitions use acquired manual sync, not the polling loop.
- The singleton managed source accepts individual publish/delete operations,
  never full-source sync or a sync lease.
- Operator sources use the same leased coordinator through `Acquire`, with complete
  namespace/all-kind snapshots and export opt-in. A valid empty operator snapshot
  removes stale exports; partial/failed listing does not.

A source incarnation, definition generation, lease ID and authoritative database-time
expiry fence every acknowledgement and commit. Reconfiguration, deletion/recreation
or scope changes invalidate old work. Multiple instances may claim different sources;
row locking alone is not a fence for work fetched outside a transaction.

## Complete attempts and last-good data

1. Acquire the current definition and bounded lease; renew while work is active.
2. Fetch and validate a complete snapshot, preserving original payload/version data.
3. Apply source filters and compare fetch/filter hashes against the applied baseline.
4. Atomically commit the snapshot **and** successful acknowledgement under the same
   fence, or complete a no-change attempt only when the current generation has a
   matching applied baseline. A new/invalidated baseline requires a real commit,
   even if the fetch hash matches historical data.
5. On failure record an allowlisted category, preserving the last good snapshot,
   its successful counts and baseline. Cancellation/expiry cannot commit stale work.

The PostgreSQL adapter keeps serializable transactions and transactional payload
COPY/upsert, latest-pointer selection, related metadata and orphan cleanup. The
low-level writer never commits independently of its caller.

## Status and readiness

Admin source responses expose allowlisted `syncStatus` fields: `phase` (`pending`,
`syncing`, `complete`, `failed`, or `unknown`), timestamps, attempt count, successful
server/skill/plugin counts and sanitized failure category. They do not expose lease
IDs, raw backend/transport causes or private hashes. Prior success is distinct from
latest attempt failure; a fetch failure is not an empty catalog.

Scheduled runtime readiness requires its backend/discovery loop to be running,
not successful fetching of every remote source. Operator readiness additionally
requires successful initial complete fenced reconciliation, including empty startup.
Watch/backend terminal failures are returned, not swallowed. Refresh and shutdown
cancel and join old workers and inline work before closing owned resources.

## Offline cutover

Stop/drain old-binary readers and writers, back up and follow
[exposure review and explicit retirement](registry-exposure-migration.md). Schema 29
retains private historical claims plumbing, archives and receipts until the final
forward cleanup; runtime startup never supplies existing-data consent or clears
claims automatically. Do not co-own sources with an old unfenced binary.
