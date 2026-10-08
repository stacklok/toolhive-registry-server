# Embedding registry models and formats (#908)

`pkg/registry/model` and `pkg/registry/formats` can be imported from a separate Go module without importing the server's storage, app, authentication or Kubernetes packages. The module's `go.mod` still lists the standalone server's dependencies: package import isolation is not a separate lightweight module.

The public model uses the upstream MCP `ServerJSON` and ToolHive's upstream registry snapshot type rather than redefining their schemas. `model.Skill` and `model.Plugin` retain catalog version metadata (`ID`, `IsLatest`, timestamps); `formats.SkillPayload`/`PluginPayload` emit only ToolHive consumer payload fields. `formats.ServerPage`, `SkillPage`, and `PluginPage` accept **already selected** versions and a caller-owned opaque cursor. They preserve the selected order, return a count of the selected records, and never fetch, filter, deduplicate, bound, or compute continuation. For routes without a cursor, pass `""`. The current server version route does this even for a truncated history; the current skill/plugin version routes return service cursors without accepting cursor input. These behaviors remain as characterized in [the baseline](registry-contract-tests.md).

`formats.DecodeUpstream` preserves the source import validator (including its nonempty-server requirement). Managed-publication validation requires a server name in the existing reverse-DNS shape (`formats.ValidateServerName`), or nonempty namespace/name/version for a skill or plugin (`formats.ValidatePublishedSkill`/`ValidatePublishedPlugin`); arbitrary nonempty version strings remain accepted. `model.PackageFormatOCI` (`"oci"`) and `model.PackageFormatGit` (`"git"`) are the stable accepted `RegistryType` values for both skill and plugin packages. When **full payload/schema** validation is required, use the upstream payloads' `Validate()` methods after conversion, as the example does. Do not substitute that stricter schema validation into the existing publish path without a separate compatibility decision. URL, metadata, and optional-field validation in the upstream/ToolHive schemas therefore remain upstream-owned, not recreated here. HTTP path decoding and query validation remain in the HTTP layer; claims enforcement remains in the existing service until #912.

The executable [separate-module example](../pkg/registry/formats/testdata/external/consumer_test.go) validates a source snapshot, server name, skill and plugin payloads, then renders registry-compatible response envelopes for all three kinds:

```sh
cd pkg/registry/formats/testdata/external
GOWORK=off go test -mod=readonly ./...
```

The main module's `go test ./pkg/registry/formats` also runs this fixture and checks its transitive **package** imports for storage, app, config, auth, and Kubernetes packages. It does not assert a clean entire-module dependency graph.
