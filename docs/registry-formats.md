# Embedding registry models and formats

`pkg/registry/model` and `pkg/registry/formats` can be imported from a separate Go module without importing the server's storage, app, authentication or Kubernetes packages. The module's `go.mod` still lists the standalone server's dependencies: package import isolation is not a separate lightweight module.

The public model uses the upstream MCP `ServerJSON` and ToolHive's upstream registry snapshot type rather than redefining their schemas. `model.Skill` and `model.Plugin` retain catalog version metadata (`ID`, `IsLatest`, timestamps); `formats.SkillPayload`/`PluginPayload` emit only ToolHive consumer payload fields. `formats.ServerPage`, `SkillPage`, and `PluginPage` accept **already selected** versions and a caller-owned opaque cursor. They preserve the selected order, return a count of the selected records, and never fetch, filter, deduplicate, bound, or compute continuation. For routes without a cursor, pass `""`. With zero-value `httpapi.ConsumerOptions`, server history has a fixed 1000-record request and no wire continuation even when truncated; skill/plugin history preserves response cursor metadata but accepts no continuation input. A host can opt into `ConsumerOptions.PaginateHistory` to pass validated history limit/cursor controls and preserve continuations; see the [consumer HTTP guide](registry-consumer-http.md#opt-in-controls). These route bounds and continuation limits are covered by [contract tests](registry-contract-tests.md).

## Current decoding and validation

These APIs validate different representations and therefore provide different guarantees:

| Boundary | Current behavior |
| --- | --- |
| `formats.ValidateServerName` | Trims and checks the existing reverse-DNS server-name shape only. |
| `formats.ValidatePublishedSkill` / `ValidatePublishedPlugin` | Require nonempty namespace/name/version only; arbitrary nonempty version strings remain accepted. |
| `formats.DecodeUpstream` | Uses core's existing `ValidateUpstreamRegistryBytes` whole-registry validation, then decodes the snapshot with `json.Decoder.UseNumber`. Imports require at least one server with a name and description. |
| `ingest.Upstream.Fetch` | Decodes each API page with `json.Decoder.UseNumber`; `ingest.Fetcher.Fetch` subsequently validates its synthesized whole-registry snapshot and typed publication constraints before returning a result. A failed page returns no partial snapshot. |
| `persistence.ValidateSnapshot` / `Entry.Validate` | Validate typed publication identity, payload discrimination, unique versions and related records, skill/plugin payloads, and the schema/extension policy on typed reserialization. Empty replacement snapshots are permitted. |

`DecodeUpstream` and `Upstream.Fetch` retain the acceptance behavior of Go's standard JSON decoding. Duplicate object names are accepted; later values replace or merge into earlier values according to `encoding/json` semantics, including when an earlier spelling uses a JSON escape. Unicode is not normalized: composed and decomposed forms remain distinct accepted strings when the relevant schema permits them. As with `encoding/json`, invalid UTF-8 and invalid UTF-16 surrogate code points are replaced during decoding rather than independently rejected. `json.Valid` preserves `json.Unmarshal`'s rejection of malformed input and trailing JSON values before the `UseNumber` decoder runs.

Dynamic publisher metadata is decoded with `json.Decoder.UseNumber()`: accepted numbers such as `9007199254740993` and exact decimal spellings remain `json.Number` through the model and formats response serialization instead of rounding to `float64`. This is not a general lossless-content/storage promise: the typed projection drops unknown fields, optional empty fields can disappear on serialization, and formatting/escaping can change.

### Core validation and typed projections

The current dependencies are `github.com/stacklok/toolhive-core v0.0.50` and `github.com/modelcontextprotocol/registry v1.8.1`. Whole-registry validation is delegated to core's `ValidateUpstreamRegistryBytes`, including its bundled MCP server schema revision 2025-12-11 and publisher-provided extension behavior. Publisher namespace entries must be objects under that extension validation; arbitrary scalar namespace values are not accepted. Validation does not fetch an input `$schema` URL. Core's validation APIs and the typed projection serve different boundaries; callers that need a guarantee beyond the existing whole-registry or typed APIs should select the API whose documented contract provides it rather than assuming raw and typed validation are interchangeable.

The typed projection is intentionally not a copy of the original JSON. It can discard unknown fields and cannot recover duplicate keys or shapes after decoding. The upstream payloads' `Validate()` methods validate their typed serialization, as the skill/plugin example does. Do not substitute stricter validation into existing publish paths without a separate compatibility decision. HTTP path decoding and query validation remain in the HTTP layer; `ConsumerOptions.StrictQueries` is an independent, opt-in HTTP query policy and does not change JSON acceptance. Standalone Cedar admission remains outside the policy-free public catalog.

## Typed models and ordering

`model.CompareVersions` defines the shared total latest-version order: Masterminds-parseable versions rank above non-semver; parseable versions compare by semantic precedence then exact raw spelling (including prefix and build metadata); two non-semver strings compare lexically. Input acceptance and stored spellings do not change. `model.PackageFormatOCI` (`"oci"`) and `model.PackageFormatGit` (`"git"`) are the stable accepted `RegistryType` values for both skill and plugin packages.

The executable [separate-module example](../pkg/registry/formats/testdata/external/consumer_test.go) validates a source snapshot, server name, skill and plugin payloads, then renders registry-compatible response envelopes for all three kinds:

```sh
cd pkg/registry/formats/testdata/external
GOWORK=off go test -mod=readonly ./...
```

The main module's `go test ./pkg/registry/formats` also runs this fixture and checks its transitive **package** imports for storage, app, config, auth, and Kubernetes packages. It does not assert a clean entire-module dependency graph.
