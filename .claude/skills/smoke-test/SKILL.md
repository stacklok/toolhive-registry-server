---
name: smoke-test
description: Start the Registry Server via docker compose and run a suite of curl-based smoke tests covering system, read-only MCP API, admin API, and entry lifecycle scenarios.
allowed-tools: Bash, Read
argument-hint: "[keep-up]"
---

# Smoke Tests — Registry Server

Starts the Registry Server stack with `docker compose`, waits for it to be ready, then executes a suite of `curl` smoke tests organised as Gherkin scenarios. Pass `keep-up` as an argument to leave the stack running after the tests complete (useful for interactive exploration).

## Embedded Scenarios

```gherkin
Feature: Registry Server smoke tests

  Background:
    Given the Registry Server is running at http://localhost:8080
    And the "default" registry is seeded from the upstream-registry.json file source

  # ── System endpoints ──────────────────────────────────────────────────────

  Scenario: Health check
    When I GET /health
    Then the response status is 200
    And the body contains "healthy"

  Scenario: Readiness check
    When I GET /readiness
    Then the response status is 200
    And the body contains "ready"

  Scenario: Version endpoint
    When I GET /version
    Then the response status is 200
    And the body contains "version"

  Scenario: OpenAPI spec
    When I GET /openapi.json
    Then the response status is 200
    And the body contains "openapi"

  # ── MCP Registry v0.1 — read-only ─────────────────────────────────────────

  Scenario: List servers in default registry
    When I GET /registry/default/v0.1/servers
    Then the response status is 200
    And the body contains "servers"

  Scenario: List servers with search filter
    When I GET /registry/default/v0.1/servers?search=mysql
    Then the response status is 200
    And the body contains "servers"

  Scenario: List servers with limit
    When I GET /registry/default/v0.1/servers?limit=2
    Then the response status is 200
    And the count of servers is at most 2

  Scenario: List servers filtered to latest version only
    When I GET /registry/default/v0.1/servers?version=latest
    Then the response status is 200
    And the body contains "servers"

  Scenario: List versions for a known server
    When I GET /registry/default/v0.1/servers/io.github.stacklok%2Fadb-mysql-mcp-server/versions
    Then the response status is 200
    And the body contains "servers"

  Scenario: Get a specific server version (latest)
    When I GET /registry/default/v0.1/servers/io.github.stacklok%2Fadb-mysql-mcp-server/versions/latest
    Then the response status is 200
    And the body contains "io.github.stacklok/adb-mysql-mcp-server"

  Scenario: Unknown registry returns 404
    When I GET /registry/nonexistent-registry/v0.1/servers
    Then the response status is 404

  Scenario: Unknown server version returns 404
    When I GET /registry/default/v0.1/servers/com.example%2Fdoes-not-exist/versions/1.0.0
    Then the response status is 404

  # ── Admin API v1 — registries ─────────────────────────────────────────────

  Scenario: List registries
    When I GET /v1/registries
    Then the response status is 200
    And the body contains "registries"

  Scenario: Get the default registry by name
    When I GET /v1/registries/default
    Then the response status is 200
    And the body contains "default"

  Scenario: Get a nonexistent registry returns 404
    When I GET /v1/registries/does-not-exist
    Then the response status is 404

  Scenario: Create a new registry via PUT
    When I PUT /v1/registries/test-registry with an empty source list
    Then the response status is 201
    And the body contains "test-registry"

  Scenario: Update an existing registry via PUT
    When I PUT /v1/registries/test-registry again with a description
    Then the response status is 200

  Scenario: Delete an API-created registry
    When I DELETE /v1/registries/test-registry
    Then the response status is 204

  Scenario: Delete a nonexistent registry returns 404
    When I DELETE /v1/registries/does-not-exist
    Then the response status is 404

  # ── Admin API v1 — sources ────────────────────────────────────────────────

  Scenario: List sources
    When I GET /v1/sources
    Then the response status is 200
    And the body contains "sources"

  Scenario: Get a known source by name
    When I GET /v1/sources/local-file
    Then the response status is 200
    And the body contains "local-file"

  Scenario: Get a nonexistent source returns 404
    When I GET /v1/sources/does-not-exist
    Then the response status is 404

  Scenario: Create a managed source
    When I PUT /v1/sources/managed-test with body {"managed":{}}
    Then the response status is 201
    And the body contains "managed-test"

  Scenario: Delete the managed source
    When I DELETE /v1/sources/managed-test
    Then the response status is 204

  # ── Entry lifecycle — publish and delete ──────────────────────────────────

  Scenario: Publish a server version
    Given a managed source exists
    When I POST /v1/entries with a server payload
    Then the response status is 201
    And the body contains the server name

  Scenario: Publish the same server version again returns 409
    When I POST /v1/entries with the same server payload
    Then the response status is 409

  Scenario: Publish a skill version
    When I POST /v1/entries with a skill payload
    Then the response status is 201
    And the body contains the skill name

  Scenario: Delete the published server version
    When I DELETE /v1/entries/server/com.example%2Ftest-server/versions/1.0.0
    Then the response status is 204

  Scenario: Delete a nonexistent entry returns 404
    When I DELETE /v1/entries/server/com.example%2Fnope/versions/9.9.9
    Then the response status is 404

  Scenario: Publish with both server and skill in body returns 400
    When I POST /v1/entries with both server and skill fields set
    Then the response status is 400

  Scenario: Publish with neither server nor skill in body returns 400
    When I POST /v1/entries with an empty payload
    Then the response status is 400
```

---

## Steps

### 1. Start the stack

```bash
BASE_URL="http://localhost:8080"
PROJECT="thv-smoke-test"
PASS=0
FAIL=0

echo "=== Starting Registry Server stack ==="
docker compose --project-name "$PROJECT" up --build --detach --wait 2>&1 || {
  echo "ERROR: docker compose failed to start"
  docker compose --project-name "$PROJECT" logs
  exit 1
}
echo "Stack is up."
```

### 2. Wait for readiness

```bash
BASE_URL="http://localhost:8080"
echo "=== Waiting for /readiness ==="
MAX_WAIT=60
ELAPSED=0
until curl -sf "$BASE_URL/readiness" > /dev/null 2>&1; do
  sleep 2
  ELAPSED=$((ELAPSED + 2))
  if [ "$ELAPSED" -ge "$MAX_WAIT" ]; then
    echo "ERROR: Server did not become ready within ${MAX_WAIT}s"
    docker compose --project-name thv-smoke-test logs registry-api
    exit 1
  fi
  echo "  ... waiting (${ELAPSED}s)"
done
echo "Server is ready."
```

### 3. Define test helper and run scenarios

```bash
BASE_URL="http://localhost:8080"
PASS=0
FAIL=0

# ─── helper ───────────────────────────────────────────────────────────────────
# check DESCRIPTION EXPECTED_STATUS ACTUAL_STATUS BODY [GREP_PATTERN]
check() {
  local desc="$1" expected="$2" actual="$3" body="$4" pattern="${5:-}"
  local ok=true

  # Accept a range like "4xx" or "5xx"
  if echo "$expected" | grep -qE '^[45]xx$'; then
    local prefix="${expected:0:1}"
    if ! echo "$actual" | grep -qE "^${prefix}[0-9]{2}$"; then
      ok=false
    fi
  elif [ "$actual" != "$expected" ]; then
    ok=false
  fi

  if $ok && [ -n "$pattern" ]; then
    if ! echo "$body" | grep -q "$pattern"; then
      ok=false
    fi
  fi

  if $ok; then
    echo "  ✓ $desc"
    PASS=$((PASS + 1))
  else
    echo "  ✗ $desc  [expected HTTP $expected, got $actual]"
    if [ -n "$pattern" ] && ! echo "$body" | grep -q "$pattern"; then
      echo "    (body did not contain: $pattern)"
      echo "    body: $(echo "$body" | head -3)"
    fi
    FAIL=$((FAIL + 1))
  fi
}

curl_get() { curl -s -o /tmp/thv_body -w "%{http_code}" "$BASE_URL$1"; }
curl_post() { curl -s -o /tmp/thv_body -w "%{http_code}" -X POST -H "Content-Type: application/json" -d "$2" "$BASE_URL$1"; }
curl_put()  { curl -s -o /tmp/thv_body -w "%{http_code}" -X PUT  -H "Content-Type: application/json" -d "$2" "$BASE_URL$1"; }
curl_del()  { curl -s -o /tmp/thv_body -w "%{http_code}" -X DELETE "$BASE_URL$1"; }
body()      { cat /tmp/thv_body; }

# ─── System endpoints ─────────────────────────────────────────────────────────
echo ""
echo "── System endpoints ──"

SC=$(curl_get /health);    check "GET /health returns 200 with healthy" 200 "$SC" "$(body)" "healthy"
SC=$(curl_get /readiness); check "GET /readiness returns 200 with ready" 200 "$SC" "$(body)" "ready"
SC=$(curl_get /version);   check "GET /version returns 200 with version field" 200 "$SC" "$(body)" "version"
SC=$(curl_get /openapi.json); check "GET /openapi.json returns 200 with openapi field" 200 "$SC" "$(body)" "openapi"

# ─── MCP Registry v0.1 ───────────────────────────────────────────────────────
echo ""
echo "── MCP Registry v0.1 ──"

SC=$(curl_get /registry/default/v0.1/servers)
check "GET /registry/default/v0.1/servers returns 200" 200 "$SC" "$(body)" "servers"

SC=$(curl_get "/registry/default/v0.1/servers?search=mysql")
check "GET .../servers?search=mysql returns 200" 200 "$SC" "$(body)" "servers"

SC=$(curl_get "/registry/default/v0.1/servers?limit=2")
check "GET .../servers?limit=2 returns 200" 200 "$SC" "$(body)" "servers"
SERVER_COUNT=$(body | grep -o '"count":[0-9]*' | grep -o '[0-9]*' | head -1)
SERVER_COUNT="${SERVER_COUNT:-0}"
if [ "$SERVER_COUNT" -le 2 ]; then
  echo "  ✓ limit=2 returned at most 2 servers (count=$SERVER_COUNT)"
  PASS=$((PASS + 1))
else
  echo "  ✗ limit=2 returned more than 2 servers (count=$SERVER_COUNT)"
  FAIL=$((FAIL + 1))
fi

SC=$(curl_get "/registry/default/v0.1/servers?version=latest")
check "GET .../servers?version=latest returns 200" 200 "$SC" "$(body)" "servers"

SC=$(curl_get "/registry/default/v0.1/servers/io.github.stacklok%2Fadb-mysql-mcp-server/versions")
check "GET .../servers/{name}/versions returns 200" 200 "$SC" "$(body)" "servers"

SC=$(curl_get "/registry/default/v0.1/servers/io.github.stacklok%2Fadb-mysql-mcp-server/versions/latest")
check "GET .../servers/{name}/versions/latest returns 200 with name" 200 "$SC" "$(body)" "adb-mysql-mcp-server"

SC=$(curl_get /registry/nonexistent-registry/v0.1/servers)
check "GET /registry/nonexistent-registry/... returns 404" 404 "$SC" "$(body)"

SC=$(curl_get "/registry/default/v0.1/servers/com.example%2Fdoes-not-exist/versions/1.0.0")
check "GET unknown server version returns 404" 404 "$SC" "$(body)"

# ─── Admin v1 — registries ────────────────────────────────────────────────────
echo ""
echo "── Admin v1 — registries ──"

SC=$(curl_get /v1/registries)
check "GET /v1/registries returns 200" 200 "$SC" "$(body)" "registries"

SC=$(curl_get /v1/registries/default)
check "GET /v1/registries/default returns 200" 200 "$SC" "$(body)" "default"

SC=$(curl_get /v1/registries/does-not-exist)
check "GET /v1/registries/does-not-exist returns 404" 404 "$SC" "$(body)"

SC=$(curl_put /v1/registries/test-registry '{"sources":["local-file"]}')
check "PUT /v1/registries/test-registry (create) returns 201" 201 "$SC" "$(body)" "test-registry"

SC=$(curl_put /v1/registries/test-registry '{"sources":["local-file"]}')
check "PUT /v1/registries/test-registry (update) returns 200" 200 "$SC" "$(body)"

SC=$(curl_del /v1/registries/test-registry)
check "DELETE /v1/registries/test-registry returns 204" 204 "$SC" "$(body)"

SC=$(curl_del /v1/registries/does-not-exist)
check "DELETE /v1/registries/does-not-exist returns 404" 404 "$SC" "$(body)"

# ─── Admin v1 — sources ───────────────────────────────────────────────────────
echo ""
echo "── Admin v1 — sources ──"

SC=$(curl_get /v1/sources)
check "GET /v1/sources returns 200" 200 "$SC" "$(body)" "sources"

SC=$(curl_get /v1/sources/local-file)
check "GET /v1/sources/local-file returns 200" 200 "$SC" "$(body)" "local-file"

SC=$(curl_get /v1/sources/does-not-exist)
check "GET /v1/sources/does-not-exist returns 404" 404 "$SC" "$(body)"

SC=$(curl_put /v1/sources/managed-test '{"managed":{}}')
check "PUT /v1/sources/managed-test (create managed) returns 201" 201 "$SC" "$(body)" "managed-test"

# ─── Entry lifecycle ──────────────────────────────────────────────────────────
echo ""
echo "── Entry lifecycle ──"

SERVER_PAYLOAD='{"server":{"name":"com.example/test-server","version":"1.0.0","description":"Smoke test server","title":"Test Server"}}'
SKILL_PAYLOAD='{"skill":{"namespace":"com.example","name":"test-skill","version":"1.0.0","title":"Test Skill","description":"Smoke test skill"}}'

SC=$(curl_post /v1/entries "$SERVER_PAYLOAD")
check "POST /v1/entries (publish server) returns 201" 201 "$SC" "$(body)" "test-server"

SC=$(curl_post /v1/entries "$SERVER_PAYLOAD")
check "POST /v1/entries duplicate server returns 409" 409 "$SC" "$(body)"

SC=$(curl_post /v1/entries "$SKILL_PAYLOAD")
check "POST /v1/entries (publish skill) returns 201" 201 "$SC" "$(body)" "test-skill"

SC=$(curl_del "/v1/entries/server/com.example%2Ftest-server/versions/1.0.0")
check "DELETE /v1/entries/server/{name}/versions/1.0.0 returns 204" 204 "$SC" "$(body)"

SC=$(curl_del "/v1/entries/server/com.example%2Fnope/versions/9.9.9")
check "DELETE nonexistent entry returns 404" 404 "$SC" "$(body)"

SC=$(curl_post /v1/entries '{"server":{"name":"com.example/s","version":"1.0.0"},"skill":{"namespace":"x","name":"y","version":"1.0.0"}}')
check "POST /v1/entries with both server+skill returns 400" 400 "$SC" "$(body)"

SC=$(curl_post /v1/entries '{}')
check "POST /v1/entries with neither server nor skill returns 400" 400 "$SC" "$(body)"

# ─── Clean up managed source ──────────────────────────────────────────────────
SC=$(curl_del /v1/sources/managed-test)
check "DELETE /v1/sources/managed-test returns 204" 204 "$SC" "$(body)"

# ─── Summary ──────────────────────────────────────────────────────────────────
echo ""
echo "══════════════════════════════════════"
echo "  Results: $PASS passed, $FAIL failed"
echo "══════════════════════════════════════"
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
```

### 4. Stop the stack

```bash
KEEP_UP="${ARGUMENTS:-}"
PROJECT="thv-smoke-test"
if [ "$KEEP_UP" = "keep-up" ]; then
  echo ""
  echo "Stack is still running (keep-up mode)."
  echo "  API:  http://localhost:8080"
  echo "  Logs: docker compose --project-name $PROJECT logs -f registry-api"
  echo "  Stop: docker compose --project-name $PROJECT down -v"
else
  echo ""
  echo "=== Stopping stack ==="
  docker compose --project-name "$PROJECT" down -v
  echo "Stack stopped and volumes removed."
fi
```
