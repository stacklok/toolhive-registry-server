# Registry Sync

The ToolHive Registry Server automatically synchronizes data from external sources into its local database. This document describes how the sync process works, what states a source can be in, and how sync is scheduled and triggered.

## Overview

Each configured data source has an associated sync record that tracks the current state of synchronization. The server runs a background coordinator that continuously checks which sources need syncing and processes them one at a time.

Sources fall into two categories:

- **Synced sources** (`git`, `api`, `file`): Automatically synchronized from an external data source on a configurable interval.
- **Non-synced sources** (`managed`, `kubernetes`): Data is managed directly via the API or discovered through other means. These sources do not participate in the sync loop.

## Sync Status

Every source has a sync status that reflects the outcome of the most recent sync operation.

| Status | Description |
|--------|-------------|
| `Syncing` | A sync operation is currently in progress |
| `Complete` | The last sync completed successfully |
| `Failed` | The last sync failed; the source will be retried |

### State Machine

```
                   ┌──────────────────────────────────────┐
                   │            Server Startup            │
                   └──────────────────┬───────────────────┘
                                      │
              ┌───────────────────────┴────────────────────────┐
              │ non-synced source                              │ synced source
              │ (managed / kubernetes)                         │ (git / api / file)
              ▼                                                │
         ┌──────────┐                                          │ no existing row
         │ Complete │                                          ▼
         └──────────┘                                   ┌──────────┐
              │                                         │  Failed  │◄──────────┐
              │ stays here forever                      └────┬─────┘           │
              │ (no sync loop)                               │                 │ failure
                                                             │ coordinator     │
                                                             │ picks up        │
                                                             ▼                 │
                                                      ┌───────────┐            │
                                               ┌─────►│  Syncing  │            │
                                               │      └─────┬─────┘            │
                                               │            │                  │
                                               │     ┌──────┴──────┐           │
                                               │  success        failure ──────┘
                                               │     │
                                               │     ▼
                                               │ ┌──────────┐
                                               │ │ Complete │
                                               │ └────┬─────┘
                                               │      │ interval elapsed
                                               └──────┘ or data changed
```

The sync record also includes:
- The timestamp of the last sync attempt and last successful sync
- The number of consecutive failures since the last success
- The server count from the last successful sync
- A hash of the last synced data, used for change detection

## Sync Scheduling

The coordinator polls for pending work every **two minutes**, with a small random jitter applied to each interval. This prevents multiple server instances from hitting the database simultaneously.

When selecting a source to sync, the coordinator always picks the source with the oldest successful sync first (sources that have never synced are prioritized). Row-level locking ensures that multiple instances of the server can run concurrently without processing the same source twice.

## When Sync Is Triggered

A sync is triggered when any of the following conditions are true:

| Condition | Description |
|-----------|-------------|
| Initial sync | The source has never been successfully synced, or its last sync failed |
| Filter changed | The source's filter configuration has changed since the last sync |
| Interval elapsed | Enough time has passed since the last sync attempt, as defined by the source's configured sync interval |
| Data changed | The upstream data has changed since the last sync (detected by hash comparison) |

If a source is currently syncing (`Syncing` status), it is skipped until the in-progress operation completes.

## Sync Process

When a source is selected for sync:

1. **Fetch**: Data is retrieved from the upstream source (git repository, API endpoint, or file).
2. **Filter**: If the source has a filter configuration, it is applied to the fetched data.
3. **Store**: The processed data is written to the database in a single atomic transaction. Existing entries are updated, and entries no longer present in the source are removed.
4. **Update status**: The sync record is updated with the outcome — success or failure, along with the new data hash and server count.

If the sync fails at any stage, the source is marked as `Failed` and will be retried on the next coordinator cycle. There is no permanent error state; every failed source is eligible for retry.

## Source Types

### Git

Clones or fetches a remote git repository and reads a JSON data file at a configured path. Supports branch, tag, and commit SHA references. Authentication is supported via a password file.

### API

Fetches server and version data from a remote MCP Registry API endpoint over HTTP or HTTPS. Follows the standard registry API specification.

### File

Reads data from a local file path, a remote URL, or an inline data string (for API-created sources).

### Managed

Data is written directly through the administration API. The server maintains the data; there is no external source to sync from.

## Sync Interval Configuration

The sync interval for each source is configured as a duration string (for example, `30m` or `2h`). This controls how frequently the coordinator will check whether the source's data has changed and trigger a new sync if needed.

Sources with no configured interval are only synced when first created or when their filter configuration changes.

## Multi-Instance Behavior

The sync coordinator is designed to run safely across multiple instances of the server. Database-level row locking (`FOR UPDATE SKIP LOCKED`) ensures that only one instance processes a given source at a time. Other instances skip locked rows and process the next available source instead.
