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
- The number of attempts since the last success (reset to 0 when a sync succeeds)
- The entry counts (servers, skills, plugins) from the last successful sync
- A hash of the last synced data, used for change detection

## When Sync Is Triggered

### Polled sources (`git`, `api`, `file` path or URL)

A polled source is due when any of the following is true:

| Condition | Description |
|-----------|-------------|
| Never synced | The source has no sync record yet |
| Definition changed | The source's configuration (location, filter, interval, …) has changed since the last attempt |
| Interval elapsed | At least `syncPolicy.interval` has passed since the last attempt finished |
| Retry after failure | The last attempt failed and **2 minutes** or `syncPolicy.interval`, whichever is shorter, have passed since it finished |

A failing source is retried every two minutes (or on its interval, if shorter) until a sync succeeds, as in v1.5.4.

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
