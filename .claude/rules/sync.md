---
paths:
  - "internal/sync/**/*.go"
  - "internal/kubernetes/**/*.go"
  - "pkg/registry/ingest/**/*.go"
  - "pkg/registry/reconcile/**/*.go"
  - "pkg/registry/operator/**/*.go"
---

# Ingestion and Reconciliation

## 1. Atomic Fenced Snapshots Preserve Last Good Data

Public reconciliation consumes `persistence.Jobs`: source incarnation, generation,
lease identity and authoritative expiry fence commit and acknowledgement in one operation.
Failure/cancellation/partial listing never becomes an empty success. PostgreSQL retains
serializable transactions, payload temp tables/COPY/upsert, latest and orphan cleanup.
`internal/sync/writer.StoreSourceTx` and `PublishSourceTx` only stage inside the caller's
transaction; no unleased Store facade, manager, state service or old coordinator exists.
Schema-29 claims plumbing/mutation guards remain until the final schema slice, not policy.

## 2. Scheduled Versus Manual Sources

ClaimNext schedules Git/API/file only; managed/operator/inline files bypass polling.
Managed publication is individual atomic publication/deletion, not full-source replacement.
Manual/operator refresh uses Acquire through the leased coordinator. `internal/sync/standalone`
supervises owned workers, refreshes definitions and joins inline/background work on shutdown.
Constructors start no work; borrowed transports, clients/managers and telemetry remain borrowed.

## 3. Operator Opt-In and Full Scope

Preserve exact `toolhive.stacklok.dev/registry-export=true` and configured export URL/description,
never silently advertise cluster-internal status URLs. Support MCPServer, VirtualMCPServer
and MCPRemoteProxy. Fetch all configured namespaces and all kinds before one source commit,
including valid empty startup snapshots. List/watch/cache/commit failures retain last good
state and cannot mark initial readiness. Namespace changes require reconstruction and fencing.
Standalone preflight checks retired authz-claims annotation presence before conversion;
public conversion is identity-free. Preserve service-account RBAC.
The same-name cross-kind operator identity decision (including differing versions) remains
pending user agreement. Do not change collision behavior or describe it as resolved.

## 4. Validate Imports Before Returning

`ingest.Fetcher` returns only complete validated upstream snapshots and change hashes.
Malformed/empty import-format responses fail without exposing remote/credential details;
operator replacement may legitimately be empty. Source-specific IO and secret loading are
explicit borrowed dependencies. Git/file hash original bytes; API hashes canonical Data.
Validation/fetch failure must preserve the applied baseline and previous successful snapshot.
