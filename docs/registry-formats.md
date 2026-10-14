# Embedding registry models and formats

`pkg/registry/model` and `pkg/registry/formats` can be imported from a separate Go module without importing the server's storage, app, authentication or Kubernetes packages. The module's `go.mod` still lists the standalone server's dependencies: package import isolation is not a separate lightweight module.

The public model uses the upstream MCP `ServerJSON` and ToolHive's upstream registry snapshot type rather than redefining their schemas. `model.Skill` and `model.Plugin` retain catalog version metadata (`ID`, `IsLatest`, timestamps); `formats.SkillPayload`/`PluginPayload` emit only ToolHive consumer payload fields. `formats.ServerPage`, `SkillPage`, and `PluginPage` accept **already selected** versions and a caller-owned opaque cursor. They preserve the selected order, return a count of the selected records, and never fetch, filter, deduplicate, bound, or compute continuation. For routes without a cursor, pass `""`. With zero-value `httpapi.ConsumerOptions`, server history has a fixed 1000-record request and no wire continuation even when truncated; skill/plugin history preserves response cursor metadata but accepts no continuation input. A host can opt into `ConsumerOptions.PaginateHistory` to pass validated history limit/cursor controls and preserve continuations; see the [consumer HTTP guide](registry-consumer-http.md#opt-in-controls). These route bounds and continuation limits are covered by [contract tests](registry-contract-tests.md).

## Current validation capabilities and dependencies

These guarantees are distinct; name or typed-publication validation is not validation of the original JSON:

| Boundary | Current guarantee |
| --- | --- |
| `formats.ValidateServerName` | Trims and checks the existing reverse-DNS server-name shape only. |
| `formats.ValidatePublishedSkill` / `ValidatePublishedPlugin` | Require nonempty namespace/name/version only; arbitrary nonempty version strings remain accepted. |
| `formats.DecodeUpstream` | Checks **original bytes** for strict JSON (one complete JSON value, unique decoded object names, valid UTF-8, and string escapes that decode to valid Unicode code points), then core's whole-registry schema and mandatory extension policy, then typed decoding. Imports require at least one server with a name and description. |
| `ingest.Upstream.Fetch` | Checks every **original API page** for the same strict JSON guarantee before typed normalization; preserves dynamic metadata numbers. It does **not** validate original raw server schemas. `ingest.Fetcher.Fetch` subsequently validates the synthesized whole-registry snapshot and typed publication constraints before returning a result. |
| `persistence.ValidateSnapshot` / `Entry.Validate` | Validate typed publication identity, payload discrimination, unique versions and related records, skill/plugin payloads, and the schema/extension policy on typed reserialization. Empty replacement snapshots are permitted. Original duplicate keys and invalid shapes already lost by typed decoding cannot be recovered or rejected here. |

Both raw decode boundaries use Go 1.27 `encoding/json/jsontext.Value.IsValid` with duplicate-name and invalid-UTF-8 acceptance explicitly disabled before core validation or typed normalization. They reject duplicate object names at every nesting level (including escaped-equivalent names), malformed JSON, invalid UTF-8, string escapes that do not decode to valid Unicode code points, and trailing JSON values. Neither check modifies the original input bytes. A failed API page, including one after earlier successful pages, returns no partial snapshot; the fetcher returns no snapshot/hash to apply, preserving the last successful backend snapshot.

Dynamic publisher metadata is decoded with `json.Decoder.UseNumber()`: accepted numbers such as `9007199254740993` and exact decimal spellings remain `json.Number` through the model and formats response serialization instead of rounding to `float64`. This is **not** a general lossless-content/storage promise: the typed projection drops unknown fields, optional empty fields can disappear on serialization, and formatting/escaping can change. Core v0.0.50 still parses extension metadata with `float64` internally and rejects overflow such as `1e400`, even though strict JSON accepts that number. No stronger numeric-range guarantee is available with this dependency.

### Bundled schema and extension policy

The current dependencies are `github.com/stacklok/toolhive-core v0.0.50` and `github.com/modelcontextprotocol/registry v1.8.1`. Whole-registry raw validation is delegated to core's `ValidateUpstreamRegistryBytes`, using its embedded upstream-registry schema and bundled **draft-7 MCP server schema revision 2025-12-11**. Validation does not fetch the input's `$schema` URL. This bundled artifact is **not exact official-schema parity** with upstream v1.8.1: it is missing `Package.properties.version.maxLength: 255`.

Core's whole-registry validator also **forces ToolHive extension policy**; callers cannot independently choose official server-schema validation and optional extension validation. Under that policy, entries in `_meta["io.modelcontextprotocol.registry/publisher-provided"]` are namespaced **objects**, including arbitrary custom namespace objects (with supported JSON content), not just recognized ToolHive namespaces. Recognized ToolHive extensions receive their additional checks. The official schema permits scalar namespace entries too; core's stricter policy rejects them. These behaviors are upstream-owned, not recreated locally.

### Blocked raw single-server validator and required companion release

There is currently **no public raw single-server API here** that provides official-schema validation separately from optional ToolHive extensions. Core v0.0.50's `ValidateServerJSON(bytes, false)` merely unmarshals a map; it does **not** validate the standard server schema. Passing `true` adds extension checks, not the missing standard-schema check. Core's `schemaFS` and `validateAgainstSchema` are private. Upstream registry v1.8.1 exposes no public raw schema validator; its internal typed-reserialization path cannot validate the original input. Thus an original API shape such as `"packages": null` can disappear during typed normalization before synthesized snapshot validation. Strict JSON checks do not close this schema gap.

The exact required **toolhive-core companion change** is:

1. Strengthen the **existing** `ValidateServerJSON` to strict-check the original raw JSON first, then **always** call `validateAgainstSchema(serverData, "data/server.schema.json", ...)`, regardless of the extension-validation flag. Do not validate a typed reserialization instead.
2. Keep extension checks optional and separate, reading publisher metadata from the **raw** metadata object rather than a typed projection; preserve the documented namespaced-object policy when extension checks are enabled.
3. Use `json.Decoder.UseNumber()` in the whole-registry path (including dynamic extension metadata decoding) rather than converting metadata numbers to `float64`.
4. Align the bundled 2025-12-11 draft-7 artifact with upstream registry v1.8.1, specifically adding `Package.properties.version.maxLength: 255`, and test both 255-character acceptance and 256-character rejection. Test strict original JSON rejection, raw schema-invalid shapes, extension-on/off separation, scalar official metadata versus object extension policy, and number preservation.

After companion approval and a core release, this repository needs an explicit dependency bump before it can consume that corrected validator. No synthetic registry-envelope workaround, copied schema, remote `$schema` fetch, or wrapper over the known-weak single-server dependency is supplied here. This capability remains blocked, not implemented.

## Typed models and ordering

`model.CompareVersions` defines the shared total latest-version order: Masterminds-parseable versions rank above non-semver; parseable versions compare by semantic precedence then exact raw spelling (including prefix and build metadata); two non-semver strings compare lexically. Input acceptance and stored spellings do not change. `model.PackageFormatOCI` (`"oci"`) and `model.PackageFormatGit` (`"git"`) are the stable accepted `RegistryType` values for both skill and plugin packages. The upstream payloads' `Validate()` methods validate their **typed serialization**, as the skill/plugin example does, not original raw JSON. Do not substitute stricter validation into existing publish paths without a separate compatibility decision. HTTP path decoding and query validation remain in the HTTP layer; standalone Cedar admission remains outside the policy-free public catalog.

The executable [separate-module example](../pkg/registry/formats/testdata/external/consumer_test.go) validates a source snapshot, server name, skill and plugin payloads, then renders registry-compatible response envelopes for all three kinds:

```sh
cd pkg/registry/formats/testdata/external
GOWORK=off go test -mod=readonly ./...
```

The main module's `go test ./pkg/registry/formats` also runs this fixture and checks its transitive **package** imports for storage, app, config, auth, and Kubernetes packages. It does not assert a clean entire-module dependency graph.
