# Registry Sync

The ToolHive Registry Server automatically synchronizes data from external sources into its local database. This document describes how the sync process works, what states a source can be in, and how sync is scheduled and triggered.

## Overview

Each configured data source has an associated sync record that tracks the current state of synchronization. How a source is synced depends on its type:

- **Polled sources** (`git`, `api`, and `file` with a `path` or `url`): fetched on the source's `syncPolicy.interval`.
- **Inline sources** (`file` with `data`, usually created through the admin API): synced as soon as they are created or changed. Their data lives in the source definition, so there is nothing to poll.
- **Kubernetes sources** (`kubernetes`): watch MCP server resources and sync whenever one changes.
- **Managed sources** (`managed`): written only by `POST /v1/entries` and the delete endpoints. They are never synced.

## Sync Status

Every source except a managed one has a sync status that reflects the outcome of the most recent sync operation.

| Status | Description |
|--------|-------------|
| `Syncing` | A sync operation is currently in progress |
| `Complete` | The last sync completed successfully |
| `Failed` | The last sync failed; the source will be retried |

A managed source reports `Complete` with the message `Non-synced source (type: managed)` from its first start onwards.

A Kubernetes source reports the outcome of its last sync, with its server count, like any other synced source. In v1.5.4 it always reported `Complete` with the message `Non-synced source (type: kubernetes)` and a server count of 0.

### State Machine

```
                   ┌──────────────────────────────────────┐
                   │   Source created or definition changed │
                   └──────────────────┬───────────────────┘
                                      │
              ┌───────────────────────┴────────────────────────┐
              │ managed source                                 │ any other source
              ▼                                                ▼
         ┌──────────┐                                    ┌───────────┐
         │ Complete │                             ┌─────►│  Syncing  │
         └──────────┘                             │      └─────┬─────┘
              │ stays here                        │            │
              │ (never synced)                    │     ┌──────┴──────┐
                                                  │  success       failure
                                                  │     │             │
                                                  │     ▼             ▼
                                                  │ ┌──────────┐ ┌──────────┐
                                                  │ │ Complete │ │  Failed  │
                                                  │ └────┬─────┘ └────┬─────┘
                                                  │      │            │
                                                  └──────┴────────────┘
                                                   next sync is due
                                                   (see "When Sync Is Triggered")
```

The sync record also includes:
- The start time of the last sync attempt and the finish time of the last completed sync attempt
- The number of failed attempts since the last success (reset to 0 when a sync succeeds; v1.5.4 always reported 0)
- The entry counts (servers, skills, plugins) from the last successful sync
- A hash of the last synced data, used for change detection

### What a Sync Attempt Changes

While an attempt runs, the status is `Syncing` and the start time is the
attempt's. The finish time (`lastSyncTime`) and the message of the previous
attempt stay visible until the attempt ends.

| Outcome | Status | Start time | Finish time (`lastSyncTime`) | Message | Attempt count |
|---------|--------|------------|------------------------------|---------|---------------|
| Data changed, stored | `Complete` | this attempt | now | cleared | 0 |
| Failed | `Failed` | this attempt | now | failure category | +1 |
| Data unchanged, previous attempt succeeded | unchanged | unchanged | unchanged | unchanged | unchanged |
| Data unchanged, previous attempt failed | `Complete` | this attempt | now | cleared | 0 |

An unchanged fetch after a success moves only the source's next due time, which
the record keeps separately; everything shown above is left as it was, as in
v1.5.4. If a server stops in the middle of an attempt, the attempt's lease
expires and the source is due again at once, so the next poll replaces the
leftover `Syncing` row; until then the library's job status already reports the
values from before the attempt.

## When Sync Is Triggered

### Polled sources (`git`, `api`, `file` path or URL)

A polled source is due when any of the following is true:

| Condition | Description |
|-----------|-------------|
| Never synced | The source has no sync record yet |
| Definition changed | The source's configuration (location, filter, interval, …) has changed since the last attempt |
| Interval elapsed | At least `syncPolicy.interval` has passed since the last attempt finished |
| Retry after failure | The last attempt failed and **2 minutes** or `syncPolicy.interval`, whichever is shorter, have passed since it finished |

A failing source is retried every two minutes (or on its interval, if shorter) until a sync succeeds. This is new: v1.5.4 waited the whole interval after a failure.

If the fetched data hashes the same as the last successful sync and the filter has not changed, the attempt completes without rewriting any entries.

### Inline sources

An inline source is synced when it is created and whenever its data or filter changes. Its status is `Complete` once that sync succeeds. In v1.5.4, inline sources created through the admin API were never synced and stayed `Failed` with `No previous sync status found`.

### Kubernetes sources

A Kubernetes source syncs when a watched resource is added, changed or removed, and re-lists every resource in its scope every **5 minutes** to catch anything a watch missed. A failed sync is retried after about a second. The API server ends watches routinely; the source restarts them on its own.

## Sync Scheduling

Every server instance checks for a due polled source about once a second. It takes a **lease** on the source before fetching, so only one instance syncs a given source at a time; other instances skip it and move on. A lease lasts one minute and is renewed while the sync runs, so a sync abandoned by a crashed instance is picked up again after about a minute.

When several sources are due, the one whose last attempt finished longest ago goes first; sources that have never been synced go before all others.

Changing a source's definition while it is syncing invalidates the running sync. Its result is discarded and the source is synced again with the new definition.

## Sync Process

When a source is synced:

1. **Fetch**: Data is retrieved from the upstream source (git repository, API endpoint, file, inline data or Kubernetes resources).
2. **Validate**: The data is parsed and validated. Invalid data fails the sync before anything is written.
3. **Filter**: If the source has a filter configuration, it is applied to the fetched data.
4. **Store**: The processed data is written to the database in a single serializable transaction. Existing entries are updated, and entries no longer present in the source are removed. Entry claims are written in the same transaction.
5. **Update status**: The sync record is updated with the outcome — success or failure, along with the new data hash and entry counts (servers, skills, plugins).

If the sync fails at any stage, nothing is written, the previous data stays visible, and the source is marked as `Failed`. There is no permanent error state; every failed source is retried. The status message names the failed stage (`Fetch failed`, `Registry validation failed`, `Storage failed`, `Sync cancelled`) rather than the raw error, which can contain credentials; the server log has the details.

A commit that keeps conflicting with concurrent writes, such as publishes, fails only that attempt; the source is retried like any other failure. If the database itself fails (unreachable, or missing a privilege), sync stops for every source and the server restarts it, after 1 second and then doubling up to 1 minute, logging `Source synchronization stopped; restarting` with the cause each time. Consumer reads keep serving the last synced data meanwhile.

Timeouts (`api.timeout`, `file.timeout`) apply to each HTTP request, not to the whole sync. A sync that runs longer than its job lease keeps the lease by renewing it while it works, so a large paginated API or a slow Git clone is not cut off.

## Validation

Each source type keeps the acceptance rules it had in v1.5.4. Fetched data is checked in two steps:

1. **Source policy** (before filtering, on everything fetched):

   | Source | Checks |
   |--------|--------|
   | `file`, `git` | The upstream registry format, validated against the ToolHive wire schema; at least one server; every server has a name and a description. |
   | `api` | The MCP Registry v0.1 protocol only (a `servers` array on every page). No schema check. Zero servers is a valid result and empties the source. |
   | `kubernetes` | No schema check. Every resource annotated `toolhive.stacklok.dev/registry-export: "true"` with the `registry-url` and `registry-description` annotations is exported. A resource that cannot be converted is skipped with the warning `Failed to extract ServerJSON from K8s resource, skipping`. |

2. **Storage safety** (after filtering, on what will be stored). A filter can therefore exclude a record that could not be stored. Each server, skill and plugin needs a non-empty name and version (at most 4096 bytes each), unique per kind within the source; server names must have the shape `namespace/name` (exactly one `/`, no whitespace); a skill or plugin `status`, when set, must be `active`, `deprecated` or `archived` in any case; and all text must be valid UTF-8 without NUL characters. Duplicate packages, remotes and icons within one server are not errors: the first of each is stored.

A record that fails either step fails the sync for that source only, with the category `validation`; the source keeps its previous data. Content the database itself rejects (for example an index entry that is too large) is reported the same way. Publishing through the API applies stricter rules (the MCP server name format, a required description, no duplicates, and the full wire schemas).

## Logging

The sync pipeline logs with these messages, so operators can follow each source:

| Message | Level | Fields |
|---------|-------|--------|
| `Starting sync operation` | Info | `source` |
| `Sync completed successfully` | Info | `source`, `server_count`, `skill_count`, `plugin_count`, `hash` (first 8 characters) |
| `Registry does not need sync` | Debug | `source`, `reason` (the fetched data is unchanged) |
| `Sync failed` | Error | `source`, `category` (`fetch`, `validation`, `storage`), `error` (the underlying cause) |
| `Sync cancelled` | Info | `source` (the server is shutting down) |
| `Fetched all servers from upstream API` | Info | `source`, `count` |
| `Starting git clone`, `Git clone completed`, `Git clone failed` | Info / Info / Error | `source`, `repository`, `branch`, `tag`, `commit`, `duration`, `commit_sha` |
| `Failed to start Kubernetes watch`, `Kubernetes watch failed` | Warn | `source`, `kind`, `namespace`, `reason`, `error` |

A validation `error` names the record (kind, name and version) and the reason. Logged URLs never include user information (user names, passwords, tokens), query strings or fragments, and request headers and credentials are never logged. The source status and API responses report only the category.

## Source Types

### Git

Clones or fetches a remote git repository and reads a JSON data file at a configured path. Supports branch, tag, and commit SHA references. Authentication is supported via a password file.

### API

Fetches server and version data from a remote MCP Registry API endpoint over HTTP or HTTPS. Follows the standard registry API specification.

### File

Reads data from a local file path, a remote URL, or an inline data string (for API-created sources).

### Kubernetes

Reads ToolHive MCP server resources (`MCPServer`, `VirtualMCPServer`, `MCPRemoteProxy`) that carry the `toolhive.stacklok.dev/registry-export=true` annotation. See [Kubernetes](configuration.md#kubernetes) in the configuration reference.

### Managed

Data is written directly through the administration API. The server maintains the data; there is no external source to sync from.

## Sync Interval Configuration

The sync interval for each polled source is configured as a duration string (for example, `30m` or `2h`) and is required. The source is fetched once per interval; a failed sync is retried after two minutes, or after the interval if that is shorter.

In v1.5.4 the interval was checked on a fixed two-minute cycle, so sources were fetched at most every two minutes and often later than their interval. Sources are now fetched on their interval, which can mean more upstream requests for sources with short intervals.

`syncPolicy` has no effect on inline, Kubernetes and managed sources; if one is set, it is ignored with a warning.

## Multi-Instance Behavior

Sync is designed to run safely across multiple instances of the server. Leases stored in the database ensure that only one instance syncs a given source at a time, and a lease that is not renewed expires so another instance can take over.
