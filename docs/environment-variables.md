# Environment variables and CLI settings

The server loads a YAML configuration snapshot with supported `THV_REGISTRY_*`
overrides. CLI listener/config selection is separate: use actual flags rather than
assuming every flag is bound to the environment.

## Precedence and names

For decoded configuration fields, defaults are overridden by YAML, then by
supported environment variables. Nested keys replace dots with underscores:
`database.host` becomes `THV_REGISTRY_DATABASE_HOST`. Use uppercase names;
environment lookups are case-sensitive. Arrays of sources, views, OAuth providers
and identity mappings belong in YAML, not ad hoc indexed environment variables.

`serve --auth-mode` explicitly overrides the decoded auth mode before final
validation. Global CLI Viper does **not** bind automatic environment variables:
`THV_REGISTRY_ADDRESS` and `THV_REGISTRY_CONFIG_FILE` do not select a listener or
config file. Use:

```sh
thv-registry-api serve --config /etc/registry/config.yaml \
  --address :8080 --internal-address 127.0.0.1:8081
```

`--config` is required. `--address` defaults to `:8080`; `--internal-address`
defaults to `:8081`. Probes/metrics exist only on the internal listener, with no
authentication/audit. Container orchestration may need `:8081` for pod probes, but
must restrict network exposure. Logging reads `THV_REGISTRY_LOG_LEVEL` separately,
falling back to `LOG_LEVEL` (`debug`, `info`, `warn`, `error`; default `info`). These
logging variables do not imply automatic environment binding for CLI flags.

`--initialize-empty` is explicit, default-false new-install authorization, not an
environment setting. Existing catalogs require the
[offline exposure procedure](registry-exposure-migration.md). Final config and CLI
overrides are validated before frozen Cedar and DB work. For offline review,
materialize exposure-relevant overrides in YAML/environment and retain the exact
same effective configuration through finalization and serving.

## Supported configuration overrides

Common examples:

| YAML key | Environment variable |
|---|---|
| `database.host` | `THV_REGISTRY_DATABASE_HOST` |
| `database.port` | `THV_REGISTRY_DATABASE_PORT` |
| `database.user` | `THV_REGISTRY_DATABASE_USER` |
| `database.migrationUser` | `THV_REGISTRY_DATABASE_MIGRATIONUSER` |
| `database.database` | `THV_REGISTRY_DATABASE_DATABASE` |
| `database.sslMode` | `THV_REGISTRY_DATABASE_SSLMODE` |
| `database.maxOpenConns` | `THV_REGISTRY_DATABASE_MAXOPENCONNS` |
| `auth.mode` | `THV_REGISTRY_AUTH_MODE` |
| `auth.oauth.resourceUrl` | `THV_REGISTRY_AUTH_OAUTH_RESOURCEURL` |
| `authorization.policyFile` | `THV_REGISTRY_AUTHORIZATION_POLICYFILE` |
| `authorization.allowAnonymousCatalog` | `THV_REGISTRY_AUTHORIZATION_ALLOWANONYMOUSCATALOG` |

```sh
THV_REGISTRY_DATABASE_HOST=postgres-primary.prod.svc.cluster.local \
  thv-registry-api serve --config /etc/registry/config.yaml
```

Define complete `sources[]`, `registries[]`, per-source `syncPolicy` and `filter`,
`auth.oauth.providers[]`, and `authorization.identityMappings[]` in the YAML file.
Do not change the DB target spelling while an existing-data receipt is pending.

Anonymous development requires `auth.mode: anonymous`,
`authorization.allowAnonymousCatalog: true`, admin off and a Cedar permit (included
in the built-in policy). Changing `THV_REGISTRY_AUTH_MODE` alone is not a complete
anonymous configuration. OAuth is the production default; invalid supplied tokens
never become anonymous. See [authentication](authentication.md) and
[Cedar admission](registry-cedar-authorization.md).

## Secrets and operational switches

Prefer absolute read-only `passwordFile` for Git, `clientSecretFile` for OAuth, and
PostgreSQL `PGPASSFILE` for DB passwords. DB
`THV_REGISTRY_DATABASE_PASSWORD`/`THV_REGISTRY_DATABASE_MIGRATIONPASSWORD` are
supported escape hatches and override pgpass when set; do not put their values in
shell commands, Helm ConfigMaps, committed YAML or log output. Static passwords
and `dynamicAuth` are mutually exclusive. See [database security](database.md#password-security).

Other explicit operational switches include:

- `THV_REGISTRY_WATCH_NAMESPACE`: Kubernetes namespace scope when a source omits
  namespaces; coordinate with chart RBAC scope and offline operator preflight.
- `THV_REGISTRY_INSECURE_URL`: env-only insecure OAuth URL opt-in for development,
  not a YAML field or a production recommendation.
- `THV_REGISTRY_COMPRESS_RESPONSE`: env-only gzip response compression switch.
- `THV_REGISTRY_LEADER_ELECTION_ID`: retired; nonempty values fail. Current workers
  use per-source database leases, not that leader-election setting.

Retired `THV_REGISTRY_AUTH_AUTHZ*`, `THV_REGISTRY_AUTH_ROLES*`,
`THV_REGISTRY_AUTH_PUBLICPATHS*` and source/view claims overrides are rejected by
presence, including empty values. They are not compatibility access switches.

## Compose and Helm

`CONFIG_FILE` is **Compose interpolation only** in this repository's
`docker-compose.yaml`: it selects a filename under mounted `/examples` and expands
the service's `--config` argument. It has no meaning to a directly invoked binary.

```sh
CONFIG_FILE=config-docker-multiple.yaml docker compose config --quiet
```

This only validates rendering. Startup, credentials and new/existing catalog paths
are in [Docker deployment](deployment-docker.md).

For Helm, use `config` for non-secret YAML, `extraEnv` for overrides and mounted
Secret files for credentials. A `secretKeyRef` is available if file mounts are not
possible; avoid literal passwords in values. Use the complete
[Kubernetes values example](deployment-kubernetes.md#prepare-operator-valuesyaml)
for readable pgpass and internal-only networking instead of constructing a partial
Deployment that omits auth, credentials or initialization.

## Troubleshooting

Check exact variable spelling, export scope, the selected YAML file and actual
CLI flags. Review sanitized startup logs; never dump `env`, pgpass contents or
password-bearing config to diagnose precedence. A flag is not automatically an
environment override, and a successfully rendered Compose/Helm file does not
prove final runtime configuration or database readiness.
