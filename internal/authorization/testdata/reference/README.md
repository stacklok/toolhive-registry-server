# Official Cedar reference fixtures

`results.json` was produced by executing the **official** crates.io
[`cedar-policy` 4.13.0](https://crates.io/crates/cedar-policy/4.13.0)
([versioned API](https://docs.rs/cedar-policy/4.13.0/cedar_policy/),
[official source tag](https://github.com/cedar-policy/cedar/tree/v4.13.0)).
The crate declares Rust 1.89 as its minimum; execution used the already available
Rust/Cargo 1.94.0 in the `dev` toolbox. No system package/toolchain installation or
shared-cache repair was needed. `Cargo.lock` pins the generator's dependency closure.

The generator reads the actual shipped `schema.cedarschema` and every Cedar block
in the current authorization guide, strictly validates policies, constructs
schema-checked entities/requests and calls the reference `Authorizer`. The 19 cases
cover the guide's reader/publisher/operator grant matrix, default deny, disjoint
anonymous behavior, issuer distinctions, missing optional groups, explicit forbid,
and raw Allow with diagnostic errors. Authentication's 401, mapped identity UID
construction, route evidence and application fail-closed behavior are Go boundary
contracts, not Rust authorizer behavior. `TestOfficialReferenceParity` compares
validation, decisions and diagnostic counts, then checks that the Go application
denies any diagnostic error, including a raw Allow. The intentionally ill-typed
last case cannot pass the production policy loader.

## Reproduce from the repository root

Use an existing compatible Rust toolchain. These temporary directories are uniquely
created and not deleted by this procedure. Do not repair shared caches or install a
toolchain to bypass an unavailable/incompatible environment.

```sh
work=$(mktemp -d /tmp/registry-cedar-reference-XXXXXXXX)
CARGO_HOME="$work/cargo" CARGO_TARGET_DIR="$work/target" \
  cargo run --locked --manifest-path internal/authorization/testdata/reference/Cargo.toml \
  -- . > "$work/results.json"
diff -u internal/authorization/testdata/reference/results.json "$work/results.json"
GOWORK=off go test -race ./internal/authorization -run '^TestOfficialReferenceParity$'
```

Within the toolbox, prefix the command with `toolbox run --container dev`, or run
this procedure in a `dev` shell. Updating policy examples, schema or cases requires
actual regeneration with the pinned reference, not hand-adjusted expected results.
SHA-256 provenance binds all three inputs; routine Go CI needs no Rust build.

## View-scoped read upgrade

The current schema and examples were regenerated with the pinned official reference
for view-scoped `catalog.read`. Engineering is allowed in engineering and denied in
finance; all other actions remain instance-scoped. This replaces the earlier fixture
set. Rust remains reference-validation tooling only, never a runtime dependency.

This is bounded evidence for the shipped policies/schema and cases, **not universal
language/validator equivalence**. The official cedar-go v1.8.0
[README](https://github.com/cedar-policy/cedar-go/blob/v1.8.0/README.md#comparison-to-the-rust-implementation)
labels validation in `x/exp/schema` experimental and outside stable API guarantees.
The Rust version is independently pinned, not inferred from the Go module version.
The shipped common syntax was accepted and executed by both. Any future mismatch
needs security review before a runtime change; fixtures do not replace that review.
