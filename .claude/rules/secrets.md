---
paths:
  - "internal/auth/**/*.go"
  - "internal/config/**/*.go"
  - "internal/db/**/*.go"
---

# Secrets and Sensitive Config

Applies to anywhere credentials or secrets are loaded, stored, or logged. File-based
secrets with symlink resolution is the baseline; env-var escape hatches exist but are not
the recommended interface.

## 1. Secrets Come From File Paths, Not Env Vars

Git auth passwords, OAuth client secrets, and optionally database passwords are loaded via
a file path (`passwordFile`, `clientSecretFile`). Env-var secrets leak into `ps`, shell
history, and process dumps.

**What must hold:**
- Secret files are required to be **absolute paths**.
- `filepath.EvalSymlinks` is called before reading — this closes a class of traversal
  attack where a symlink in a config directory becomes an arbitrary-file-read primitive.
- `THV_REGISTRY_DATABASE_PASSWORD` is the env-var escape hatch for environments that can't
  mount files, but the file path is the preferred interface.
- `readSecretFromFile` in `internal/config/config.go` is the canonical implementation —
  reuse it for any new secret field.

**Detect**: new secret config fields exposed as plain strings in YAML without a `*File`
companion; file reads that skip `EvalSymlinks`; secret-file loads that accept relative
paths.

## 2. Never Log Secret-Bearing Structs Directly

`DatabaseConfig.LogValue()` implements `slog.LogValuer` to strip passwords. Any struct that
holds a secret must do the same, or be unreachable from log statements.

**Detect**: new fields on `DatabaseConfig` or similar structs that carry secrets but aren't
redacted in `LogValue()`; `slog` calls that pass a raw config struct; error-wrapping that
embeds a connection string.

**Instead**: implement `slog.LogValuer` on any struct that carries a secret and include
only presence-indicators (e.g., `has_password: true`) in the logged value. When building a
connection string for a log message, use the user-only form without credentials.

## 3. Insecure-Allow Flags Are Env-Only, Never YAML

`insecureAllowHTTP` (for OAuth issuer URLs) is loaded only from
`THV_REGISTRY_INSECURE_URL`, never from the config file. This prevents someone from
committing the flag to a repo.

**Detect**: a YAML schema field named `insecureAllowHTTP` or similar; viper bindings that
read `insecure_url` from a config file path; tests that set the flag via YAML fixtures.

**Instead**: read env-only flags directly via viper `GetBool` after `AutomaticEnv` is set,
as the current pattern does. Keep the field unexported or tagged `yaml:"-"`.

## 4. Mutually Exclusive Auth Modes Must Be Validated

`database.password` and `database.dynamicAuth` (AWS RDS IAM) are mutually exclusive. The
same goes for the migration user — `migrationPassword` vs. `dynamicAuth`. Accepting both
silently is a config ambiguity that will eventually ship wrong credentials.

**Detect**: `validateStorageConfig` changes that add a new auth source without a
mutual-exclusion check; connection-building code that picks an auth source by precedence
instead of failing on ambiguity.

**Instead**: fail loudly at config load with a clear message naming the two fields in
conflict.
