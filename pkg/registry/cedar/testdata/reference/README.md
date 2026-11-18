# Official Cedar reference fixtures

`results.json` holds the decisions of the official Rust crate
[`cedar-policy` 4.13.0](https://crates.io/crates/cedar-policy/4.13.0) for the
cases in `cases.json`, evaluated against `../../schema.cedarschema`.
`TestOfficialReferenceParity` checks that cedar-go and this package's `Parse`
agree with it on strict validation, the decision and the number of diagnostic
errors, and that the package fails closed on any diagnostic error, including a
raw Allow. Routine Go CI needs no Rust; Rust is never a runtime dependency.

The test hash-binds exactly two inputs: the schema and `cases.json`. Editing
either breaks the test until `results.json` is regenerated with the pinned
crate. Never hand-edit the expected results.

`cases.json` has two parts. `policies` maps a name to complete Cedar policy
text; each entry in `cases` names one of them, plus the action, an optional
view (default `engineering`) and the principal's attributes. Policies live in
this file, not in documentation, so editing docs never invalidates the fixtures.

## Provenance of the current results

The results were produced with cedar-policy 4.13.0 for PR #946, where the
generator read the `group-grants` policy from the Cedar blocks of
`docs/registry-cedar-authorization.md` rather than from `cases.json`. When the
fixtures moved here, that policy text was copied byte-for-byte into
`cases.json` (its SHA-256, `e2a3c4d67ac1d067f4fdb99cb167f5bf618e8deb9ef9b66e8b3669ee92458fea`,
matches the `examples_sha256` the Rust generator recorded), the schema is
byte-identical (same `schema_sha256`), and every case, attribute set and
expected result is unchanged. Only `cases_sha256` was re-pinned, without
re-running Rust, because no Rust toolchain was available at the time. The next
regeneration replaces this note's caveat with a fresh run.

## Reproduce from the repository root

Use an existing Rust toolchain (the crate needs Rust 1.89 or newer).
`Cargo.lock` pins the generator's dependency closure.

```sh
work=$(mktemp -d /tmp/registry-cedar-reference-XXXXXXXX)
CARGO_HOME="$work/cargo" CARGO_TARGET_DIR="$work/target" \
  cargo run --locked --manifest-path pkg/registry/cedar/testdata/reference/Cargo.toml \
  -- . > "$work/results.json"
diff -u pkg/registry/cedar/testdata/reference/results.json "$work/results.json"
go test -race ./pkg/registry/cedar -run '^TestOfficialReferenceParity$'
```

This is bounded evidence for these policies, this schema and these cases, not
general equivalence between cedar-go and the Rust implementation. cedar-go
labels its `x/exp/schema` validation experimental. Any mismatch needs a security
review before a runtime change.
