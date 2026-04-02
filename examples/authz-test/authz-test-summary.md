# Authorization E2E Test Suite

## Auth/Authz Test Matrix

| auth | authz | test script | config | behavior |
|------|-------|-------------|--------|----------|
| no   | no    | smoke-test skill | `examples/config-file.yaml` | No checks (anonymous mode) |
| yes  | no    | `test-auth-only.sh` | `config-auth-only.yaml` | Token validated, no claim restrictions |
| yes  | yes   | `test-authz.sh` | `config-authz-test.yaml` | Token validated + claims checked |

## test-authz.sh — Full Authorization Tests

### Test Categories

| # | Category | What's Verified | Scenarios |
|---|----------|----------------|-----------|
| 1 | System endpoints | `/health`, `/readiness`, OAuth metadata accessible without auth | 3 |
| 2 | Unauthenticated access | Consumer and admin APIs reject requests without JWT (401); malformed/empty tokens rejected | 5 |
| 3 | Registry access gate | JWT must cover registry claims; outsider blocked; super-admin bypasses | 12 |
| 4 | Per-user entry filtering | Same registry shows different entries per user based on team claims | 12 |
| 5 | Admin source listing | Sources filtered by caller's claims; writers blocked (403); super-admin sees all | 8 |
| 6 | Admin registry listing | Registries filtered by caller's claims; writers blocked (403); super-admin sees all | 6 |
| 7 | Publish claim validation | Published claims must be subset of publisher's JWT; wrong role blocked; duplicate publish rejected | 5 |
| 8 | Published entry visibility | Published entries only visible to users whose JWT covers entry claims | 6 |
| 9 | Delete authorization | Can't delete entries with claims outside your JWT; super-admin can; re-delete returns 404 | 4 |
| 10 | Config-managed protection | Config-defined sources/registries immutable via API (403) | 3 |
| | **Total** | **5 personas, 10 categories** | **64** |

### User Personas

| Persona | JWT Claims | Roles |
|---------|-----------|-------|
| super-admin | org=acme, role=super-admin | superAdmin (full bypass) |
| platform-admin | org=acme, team=platform, role=admin | manageSources, manageRegistries |
| platform-writer | org=acme, team=platform, role=writer | manageEntries |
| data-writer | org=acme, team=data, role=writer | manageEntries |
| outsider | org=contoso, role=admin | manageSources, manageRegistries (wrong org) |

## test-auth-only.sh — Auth-Only Tests (No Authz)

### Test Categories

| # | Category | What's Verified | Scenarios |
|---|----------|----------------|-----------|
| 1 | System endpoints | Public endpoints accessible without auth | 3 |
| 2 | Unauthenticated access | Requests without valid token rejected (401) | 3 |
| 3 | Authenticated access | Any valid token grants access (no claim checks) | 3 |
| 4 | Admin API | Admin endpoints accessible with any valid token | 4 |
| 5 | Publish/delete | Any authenticated user can publish and delete | 2 |
| | **Total** | **2 personas, 5 categories** | **15** |

## How to Run

### Full authz test (auth=yes, authz=yes)
```bash
docker compose -f docker-compose-authz.yaml up --build -d
./examples/authz-test/test-authz.sh
docker compose -f docker-compose-authz.yaml down -v
```

### Auth-only test (auth=yes, authz=no)
```bash
# Override the config to use auth-only mode
docker compose -f docker-compose-authz.yaml up --build -d
# Then exec into or override the registry-api to use config-auth-only.yaml
# Or update docker-compose-authz.yaml command temporarily
./examples/authz-test/test-auth-only.sh
docker compose -f docker-compose-authz.yaml down -v
```