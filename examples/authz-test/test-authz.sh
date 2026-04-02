#!/usr/bin/env bash
# Authorization smoke tests for the ToolHive Registry Server
#
# This test covers the "auth=yes, authz=yes" row of the auth/authz matrix:
#
#   auth | authz | behavior
#   -----|-------|----------------------------------------------------
#   no   | no    | No checks (anonymous mode, see smoke-test skill)
#   yes  | no    | Token validated, no claim-based restrictions
#   yes  | yes   | Token validated + claims checked  <-- THIS TEST
#
# Prerequisites:
#   docker compose -f docker-compose-authz.yaml up --build
#
# Usage:
#   ./examples/authz-test/test-authz.sh
#
# User personas:
#   super-admin      — org=acme, role=super-admin           (full access)
#   platform-admin   — org=acme, team=platform, role=admin  (manage sources/registries, platform scope)
#   platform-writer  — org=acme, team=platform, role=writer (publish entries, platform scope)
#   data-writer      — org=acme, team=data, role=writer     (publish entries, data scope)
#   outsider         — org=contoso, role=admin              (no access to acme resources)

set -euo pipefail

BASE_URL="${REGISTRY_URL:-http://localhost:8080}"
# mock-oauth2-server uses SERVER_HOSTNAME=mock-oauth2-server so all tokens carry
# iss=http://mock-oauth2-server:8888/default. From the host, we resolve this to 127.0.0.1.
OAUTH_URL="${OAUTH_URL:-http://mock-oauth2-server:8888}"
CURL_RESOLVE="--resolve mock-oauth2-server:8888:127.0.0.1"

PASS=0
FAIL=0

# --- Helpers ---

red()    { printf "\033[31m%s\033[0m" "$*"; }
green()  { printf "\033[32m%s\033[0m" "$*"; }
yellow() { printf "\033[33m%s\033[0m" "$*"; }
bold()   { printf "\033[1m%s\033[0m" "$*"; }

# Get a JWT for a given persona (uses authorization_code grant with matching code)
get_token() {
  local persona="$1"
  local token
  token=$(curl -sf ${CURL_RESOLVE} -X POST "${OAUTH_URL}/default/token" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "grant_type=authorization_code&code=${persona}&client_id=test&client_secret=secret&redirect_uri=http://localhost/callback" \
    | jq -r '.access_token')
  if [ -z "$token" ] || [ "$token" = "null" ]; then
    echo "ERROR: Failed to get token for persona: $persona" >&2
    return 1
  fi
  echo "$token"
}

# Run a test case
# Usage: check "Test name" <expected_status> <curl_args...>
check() {
  local name="$1"
  local expected="$2"
  shift 2

  local response status body
  response=$(curl -sf -w "\n%{http_code}" "$@" 2>/dev/null) || response=$(curl -s -w "\n%{http_code}" "$@")
  status=$(echo "$response" | tail -n1)
  body=$(echo "$response" | sed '$d')

  if [ "$status" = "$expected" ]; then
    PASS=$((PASS + 1))
    printf "  $(green PASS)  %-70s [%s]\n" "$name" "$status"
  else
    FAIL=$((FAIL + 1))
    printf "  $(red FAIL)  %-70s [got %s, expected %s]\n" "$name" "$status" "$expected"
    # Print first 200 chars of body for debugging
    echo "        $(echo "$body" | head -c 200)"
  fi
}

# Run a test and also verify body contains a string
check_body() {
  local name="$1"
  local expected_status="$2"
  local expected_body="$3"
  shift 3

  local response status body
  response=$(curl -sf -w "\n%{http_code}" "$@" 2>/dev/null) || response=$(curl -s -w "\n%{http_code}" "$@")
  status=$(echo "$response" | tail -n1)
  body=$(echo "$response" | sed '$d')

  if [ "$status" = "$expected_status" ] && echo "$body" | grep -q "$expected_body"; then
    PASS=$((PASS + 1))
    printf "  $(green PASS)  %-70s [%s, body ok]\n" "$name" "$status"
  elif [ "$status" != "$expected_status" ]; then
    FAIL=$((FAIL + 1))
    printf "  $(red FAIL)  %-70s [got %s, expected %s]\n" "$name" "$status" "$expected_status"
    echo "        $(echo "$body" | head -c 200)"
  else
    FAIL=$((FAIL + 1))
    printf "  $(red FAIL)  %-70s [%s, body missing '%s']\n" "$name" "$status" "$expected_body"
    echo "        $(echo "$body" | head -c 200)"
  fi
}

# Count servers in response and check against expected
check_server_count() {
  local name="$1"
  local expected_count="$2"
  shift 2

  local response status body count
  response=$(curl -sf -w "\n%{http_code}" "$@" 2>/dev/null) || response=$(curl -s -w "\n%{http_code}" "$@")
  status=$(echo "$response" | tail -n1)
  body=$(echo "$response" | sed '$d')
  count=$(echo "$body" | jq '.servers | length' 2>/dev/null || echo "?")

  if [ "$status" = "200" ] && [ "$count" = "$expected_count" ]; then
    PASS=$((PASS + 1))
    printf "  $(green PASS)  %-70s [%s servers]\n" "$name" "$count"
  else
    FAIL=$((FAIL + 1))
    printf "  $(red FAIL)  %-70s [status=%s, count=%s, expected %s]\n" "$name" "$status" "$count" "$expected_count"
    if [ "$count" != "$expected_count" ] && [ "$count" != "?" ]; then
      echo "        servers: $(echo "$body" | jq -r '[.servers[].name] | join(", ")' 2>/dev/null || echo "$body" | head -c 200)"
    fi
  fi
}

# Count items at a given jq path and check against expected
# Usage: check_list_count "Test name" ".jq.path" <expected_count> <curl_args...>
check_list_count() {
  local name="$1"
  local jq_path="$2"
  local expected_count="$3"
  shift 3

  local response status body count
  response=$(curl -sf -w "\n%{http_code}" "$@" 2>/dev/null) || response=$(curl -s -w "\n%{http_code}" "$@")
  status=$(echo "$response" | tail -n1)
  body=$(echo "$response" | sed '$d')
  count=$(echo "$body" | jq "${jq_path} | length" 2>/dev/null || echo "?")

  if [ "$status" = "200" ] && [ "$count" = "$expected_count" ]; then
    PASS=$((PASS + 1))
    printf "  $(green PASS)  %-70s [%s items]\n" "$name" "$count"
  else
    FAIL=$((FAIL + 1))
    printf "  $(red FAIL)  %-70s [status=%s, count=%s, expected %s]\n" "$name" "$status" "$count" "$expected_count"
    echo "        $(echo "$body" | head -c 200)"
  fi
}

section() {
  echo ""
  bold "=== $1 ==="
  echo ""
}

# --- Preflight ---

section "Preflight"

echo "  Checking services..."

# Check mock-oauth2-server
if ! curl -sf ${CURL_RESOLVE} "${OAUTH_URL}/default/.well-known/openid-configuration" > /dev/null 2>&1; then
  echo "  $(red ERROR): mock-oauth2-server not reachable at ${OAUTH_URL}"
  echo "  Run: docker compose -f docker-compose-authz.yaml up --build"
  exit 1
fi
echo "  $(green OK) mock-oauth2-server at ${OAUTH_URL}"

# Check registry server
if ! curl -sf "${BASE_URL}/health" > /dev/null 2>&1; then
  echo "  $(red ERROR): Registry server not reachable at ${BASE_URL}"
  echo "  Run: docker compose -f docker-compose-authz.yaml up --build"
  exit 1
fi
echo "  $(green OK) Registry server at ${BASE_URL}"

# Fetch tokens for all personas
echo ""
echo "  Fetching tokens..."
TOKEN_SUPER=$(get_token "super-admin")
TOKEN_PLATFORM_ADMIN=$(get_token "platform-admin")
TOKEN_PLATFORM_WRITER=$(get_token "platform-writer")
TOKEN_DATA_WRITER=$(get_token "data-writer")
TOKEN_OUTSIDER=$(get_token "outsider")
echo "  $(green OK) All tokens obtained"

# Wait for initial sync to complete
echo ""
echo "  Waiting for initial sync (all sources)..."
# The sync coordinator processes one source per cycle. With 3 file sources
# it may take multiple cycles. Wait until all syncs complete.
MAX_WAIT=120
WAITED=0
while [ "$WAITED" -lt "$MAX_WAIT" ]; do
  # Check if platform-tools has synced by trying to find an entry from it
  COUNT=$(curl -sf -H "Authorization: Bearer $TOKEN_PLATFORM_WRITER" "${BASE_URL}/registry/acme-platform/v0.1/servers" 2>/dev/null | jq '.metadata.count // 0' 2>/dev/null || echo "0")
  if [ "$COUNT" -gt 0 ] 2>/dev/null; then
    break
  fi
  sleep 5
  WAITED=$((WAITED + 5))
  printf "    ... waiting (%ds)\n" "$WAITED"
done
if [ "$WAITED" -ge "$MAX_WAIT" ]; then
  echo "  $(yellow WARN) Timed out waiting for all sources to sync"
else
  echo "  $(green OK) All sources synced (${WAITED}s)"
fi
echo "  $(green OK) Ready"

# --- Pre-test cleanup for re-runnability ---
echo ""
echo "  Cleaning up entries from previous runs..."
# Silently delete entries that may be left over from a previous failed run
curl -sf -X DELETE -H "Authorization: Bearer ${TOKEN_SUPER}" \
  "${BASE_URL}/v1/entries/server/com.acme%2Fcustom-linter/versions/1.0.0" > /dev/null 2>&1 || true
curl -sf -X DELETE -H "Authorization: Bearer ${TOKEN_SUPER}" \
  "${BASE_URL}/v1/entries/server/com.acme%2Fdata-analyzer/versions/1.0.0" > /dev/null 2>&1 || true
echo "  $(green OK) Cleanup done"

# ============================================================================
# 1. SYSTEM ENDPOINTS (no auth required)
# ============================================================================
section "1. System endpoints (public, no auth)"

check "Health check" 200 \
  "${BASE_URL}/health"

check "Readiness check" 200 \
  "${BASE_URL}/readiness"

check_body "Well-known OAuth protected resource" 200 "authorization_servers" \
  "${BASE_URL}/.well-known/oauth-protected-resource"

# ============================================================================
# 2. UNAUTHENTICATED ACCESS (should be rejected)
# ============================================================================
section "2. Unauthenticated access (should be 401)"

check "Consumer API without token" 401 \
  "${BASE_URL}/registry/acme-all/v0.1/servers"

check "Admin list sources without token" 401 \
  "${BASE_URL}/v1/sources"

check "Admin list registries without token" 401 \
  "${BASE_URL}/v1/registries"

check "Malformed Bearer token" 401 \
  -H "Authorization: Bearer not-a-valid-jwt" \
  "${BASE_URL}/registry/acme-all/v0.1/servers"

check "Empty Bearer token" 401 \
  -H "Authorization: Bearer " \
  "${BASE_URL}/registry/acme-all/v0.1/servers"

# ============================================================================
# 3. REGISTRY ACCESS GATE
# ============================================================================
section "3. Registry access gate"

# acme-all requires org=acme
check "platform-writer accesses acme-all (org=acme)" 200 \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_WRITER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers"

check "data-writer accesses acme-all (org=acme)" 200 \
  -H "Authorization: Bearer ${TOKEN_DATA_WRITER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers"

# acme-platform requires org=acme AND team=platform
check "platform-writer accesses acme-platform (team=platform)" 200 \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_WRITER}" \
  "${BASE_URL}/registry/acme-platform/v0.1/servers"

check "data-writer blocked from acme-platform (team=data != platform)" 403 \
  -H "Authorization: Bearer ${TOKEN_DATA_WRITER}" \
  "${BASE_URL}/registry/acme-platform/v0.1/servers"

# acme-data requires org=acme AND team=data
check "data-writer accesses acme-data (team=data)" 200 \
  -H "Authorization: Bearer ${TOKEN_DATA_WRITER}" \
  "${BASE_URL}/registry/acme-data/v0.1/servers"

check "platform-writer blocked from acme-data (team=platform != data)" 403 \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_WRITER}" \
  "${BASE_URL}/registry/acme-data/v0.1/servers"

# Outsider (org=contoso) blocked from all acme registries
check "outsider blocked from acme-all (org=contoso)" 403 \
  -H "Authorization: Bearer ${TOKEN_OUTSIDER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers"

check "outsider blocked from acme-platform" 403 \
  -H "Authorization: Bearer ${TOKEN_OUTSIDER}" \
  "${BASE_URL}/registry/acme-platform/v0.1/servers"

# Super-admin bypasses all gates
check "super-admin accesses acme-all" 200 \
  -H "Authorization: Bearer ${TOKEN_SUPER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers"

check "super-admin accesses acme-platform (bypass)" 200 \
  -H "Authorization: Bearer ${TOKEN_SUPER}" \
  "${BASE_URL}/registry/acme-platform/v0.1/servers"

check "super-admin accesses acme-data (bypass)" 200 \
  -H "Authorization: Bearer ${TOKEN_SUPER}" \
  "${BASE_URL}/registry/acme-data/v0.1/servers"

# Nonexistent registry
check "nonexistent registry returns 404" 404 \
  -H "Authorization: Bearer ${TOKEN_SUPER}" \
  "${BASE_URL}/registry/does-not-exist/v0.1/servers"

# ============================================================================
# 4. PER-USER ENTRY FILTERING
# ============================================================================
section "4. Per-user entry filtering within acme-all"

# In acme-all, platform-writer (team=platform) should see:
#   - shared-catalog entries (claims: org=acme, no team) -> visible (absent key = open)
#   - platform-tools entries (claims: org=acme, team=platform) -> visible
#   - data-tools entries (claims: org=acme, team=data) -> NOT visible (team mismatch)
# So: shared-catalog servers + 2 platform servers, but NOT 2 data servers

# In acme-all, data-writer (team=data) should see:
#   - shared-catalog entries -> visible
#   - platform-tools entries -> NOT visible
#   - data-tools entries -> visible

# We can't predict exact count from shared-catalog (it has many), but we can check
# that platform-writer sees platform tools and data-writer sees data tools

check_body "platform-writer sees deploy-helper in acme-all" 200 "deploy-helper" \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_WRITER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers?search=deploy-helper"

check_body "platform-writer sees infra-scanner in acme-all" 200 "infra-scanner" \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_WRITER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers?search=infra-scanner"

check_server_count "platform-writer does NOT see data-pipeline in acme-all" 0 \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_WRITER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers?search=data-pipeline"

check_server_count "platform-writer does NOT see ml-trainer in acme-all" 0 \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_WRITER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers?search=ml-trainer"

check_body "data-writer sees data-pipeline in acme-all" 200 "data-pipeline" \
  -H "Authorization: Bearer ${TOKEN_DATA_WRITER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers?search=data-pipeline"

check_body "data-writer sees ml-trainer in acme-all" 200 "ml-trainer" \
  -H "Authorization: Bearer ${TOKEN_DATA_WRITER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers?search=ml-trainer"

check_server_count "data-writer does NOT see deploy-helper in acme-all" 0 \
  -H "Authorization: Bearer ${TOKEN_DATA_WRITER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers?search=deploy-helper"

check_server_count "data-writer does NOT see infra-scanner in acme-all" 0 \
  -H "Authorization: Bearer ${TOKEN_DATA_WRITER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers?search=infra-scanner"

# Both should see shared-catalog entries (no team claim = open)
check_body "platform-writer sees shared-catalog entries" 200 "servers" \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_WRITER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers?search=mysql"

check_body "data-writer sees shared-catalog entries" 200 "servers" \
  -H "Authorization: Bearer ${TOKEN_DATA_WRITER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers?search=mysql"

check_body "super-admin sees deploy-helper (bypass)" 200 "deploy-helper" \
  -H "Authorization: Bearer ${TOKEN_SUPER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers?search=deploy-helper"

check_body "super-admin sees data-pipeline (bypass)" 200 "data-pipeline" \
  -H "Authorization: Bearer ${TOKEN_SUPER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers?search=data-pipeline"

# ============================================================================
# 5. ADMIN API — SOURCE LISTING (claim scoped)
# ============================================================================
section "5. Admin API — source listing (claim scoped)"

# platform-admin (org=acme, team=platform, role=admin) has manageSources role
# Should see sources whose claims they cover:
#   - shared-catalog (org=acme) -> visible
#   - platform-tools (org=acme, team=platform) -> visible
#   - data-tools (org=acme, team=data) -> NOT visible (team mismatch)
#   - internal (no claims) -> visible

check_body "platform-admin can list sources" 200 "shared-catalog" \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_ADMIN}" \
  "${BASE_URL}/v1/sources"

check "platform-admin can get shared-catalog source" 200 \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_ADMIN}" \
  "${BASE_URL}/v1/sources/shared-catalog"

check "platform-admin can get platform-tools source" 200 \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_ADMIN}" \
  "${BASE_URL}/v1/sources/platform-tools"

check "platform-admin cannot see data-tools source (404 = hidden)" 404 \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_ADMIN}" \
  "${BASE_URL}/v1/sources/data-tools"

# Writer role does not have manageSources, so source listing is blocked
check "platform-writer cannot list sources (no manageSources role)" 403 \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_WRITER}" \
  "${BASE_URL}/v1/sources"

check "data-writer cannot list sources (no manageSources role)" 403 \
  -H "Authorization: Bearer ${TOKEN_DATA_WRITER}" \
  "${BASE_URL}/v1/sources"

# Outsider (org=contoso) — has admin role but wrong org
# Can only see sources with no claims (internal managed source has no claims = open)
check_list_count "outsider sees only claimless sources (wrong org)" ".sources" 1 \
  -H "Authorization: Bearer ${TOKEN_OUTSIDER}" \
  "${BASE_URL}/v1/sources"

# Super-admin sees all
check_body "super-admin sees all sources including data-tools" 200 "data-tools" \
  -H "Authorization: Bearer ${TOKEN_SUPER}" \
  "${BASE_URL}/v1/sources"

# ============================================================================
# 6. ADMIN API — REGISTRY LISTING (claim scoped)
# ============================================================================
section "6. Admin API — registry listing (claim scoped)"

check_body "platform-admin sees acme-all registry" 200 "acme-all" \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_ADMIN}" \
  "${BASE_URL}/v1/registries"

check "platform-admin can get acme-platform registry" 200 \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_ADMIN}" \
  "${BASE_URL}/v1/registries/acme-platform"

check "platform-admin cannot see acme-data registry (404 = hidden)" 404 \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_ADMIN}" \
  "${BASE_URL}/v1/registries/acme-data"

# Writer role can list registries (read-only, claim-filtered)
check "platform-writer can list registries (read is open to authenticated)" 200 \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_WRITER}" \
  "${BASE_URL}/v1/registries"

check "data-writer can list registries (read is open to authenticated)" 200 \
  -H "Authorization: Bearer ${TOKEN_DATA_WRITER}" \
  "${BASE_URL}/v1/registries"

check_body "super-admin sees all registries including acme-data" 200 "acme-data" \
  -H "Authorization: Bearer ${TOKEN_SUPER}" \
  "${BASE_URL}/v1/registries"

# ============================================================================
# 7. PUBLISH — CLAIM VALIDATION
# ============================================================================
section "7. Publish — claim subset validation"

# platform-writer (org=acme, team=platform, role=writer) has manageEntries
# Can publish with claims that are a subset of their JWT

check_body "platform-writer publishes entry with matching claims" 201 "custom-linter" \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_WRITER}" \
  -H "Content-Type: application/json" \
  -d '{
    "claims": {"org": "acme", "team": "platform"},
    "server": {
      "name": "com.acme/custom-linter",
      "version": "1.0.0",
      "description": "Custom linting tool for platform team",
      "packages": [{"registryType": "oci", "identifier": "ghcr.io/acme/custom-linter:1.0.0", "transport": {"type": "stdio"}}]
    }
  }' \
  "${BASE_URL}/v1/entries"

# platform-writer cannot publish with claims outside their JWT
check "platform-writer cannot publish with team=finance (not in JWT)" 403 \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_WRITER}" \
  -H "Content-Type: application/json" \
  -d '{
    "claims": {"org": "acme", "team": "finance"},
    "server": {
      "name": "com.acme/finance-tool",
      "version": "1.0.0",
      "description": "Should fail",
      "packages": [{"registryType": "oci", "identifier": "ghcr.io/acme/finance:1.0.0", "transport": {"type": "stdio"}}]
    }
  }' \
  "${BASE_URL}/v1/entries"

# data-writer publishes with data team claims
check_body "data-writer publishes entry with data team claims" 201 "data-analyzer" \
  -H "Authorization: Bearer ${TOKEN_DATA_WRITER}" \
  -H "Content-Type: application/json" \
  -d '{
    "claims": {"org": "acme", "team": "data"},
    "server": {
      "name": "com.acme/data-analyzer",
      "version": "1.0.0",
      "description": "Data analysis tool",
      "packages": [{"registryType": "oci", "identifier": "ghcr.io/acme/data-analyzer:1.0.0", "transport": {"type": "stdio"}}]
    }
  }' \
  "${BASE_URL}/v1/entries"

# outsider cannot publish (role=admin maps to manageSources/manageRegistries, not manageEntries)
check "outsider cannot publish (no manageEntries role)" 403 \
  -H "Authorization: Bearer ${TOKEN_OUTSIDER}" \
  -H "Content-Type: application/json" \
  -d '{
    "claims": {"org": "contoso"},
    "server": {
      "name": "com.contoso/tool",
      "version": "1.0.0",
      "description": "Should fail",
      "packages": [{"registryType": "oci", "identifier": "ghcr.io/contoso/tool:1.0.0", "transport": {"type": "stdio"}}]
    }
  }' \
  "${BASE_URL}/v1/entries"

# Duplicate publish should fail with 409 Conflict
check "platform-writer cannot re-publish custom-linter (duplicate)" 409 \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_WRITER}" \
  -H "Content-Type: application/json" \
  -d '{
    "claims": {"org": "acme", "team": "platform"},
    "server": {
      "name": "com.acme/custom-linter",
      "version": "1.0.0",
      "description": "Duplicate publish attempt",
      "packages": [{"registryType": "oci", "identifier": "ghcr.io/acme/custom-linter:1.0.0", "transport": {"type": "stdio"}}]
    }
  }' \
  "${BASE_URL}/v1/entries"

# ============================================================================
# 8. PUBLISHED ENTRY VISIBILITY
# ============================================================================
section "8. Published entry visibility (cross-check)"

# custom-linter was published with claims org=acme, team=platform
# It should be visible to platform-writer in acme-all, but NOT to data-writer

check_body "platform-writer sees published custom-linter in acme-all" 200 "custom-linter" \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_WRITER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers?search=custom-linter"

check_server_count "data-writer does NOT see custom-linter in acme-all" 0 \
  -H "Authorization: Bearer ${TOKEN_DATA_WRITER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers?search=custom-linter"

# data-analyzer was published with claims org=acme, team=data
check_body "data-writer sees published data-analyzer in acme-all" 200 "data-analyzer" \
  -H "Authorization: Bearer ${TOKEN_DATA_WRITER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers?search=data-analyzer"

check_server_count "platform-writer does NOT see data-analyzer in acme-all" 0 \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_WRITER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers?search=data-analyzer"

# Super-admin sees both
check_body "super-admin sees custom-linter (bypass)" 200 "custom-linter" \
  -H "Authorization: Bearer ${TOKEN_SUPER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers?search=custom-linter"

check_body "super-admin sees data-analyzer (bypass)" 200 "data-analyzer" \
  -H "Authorization: Bearer ${TOKEN_SUPER}" \
  "${BASE_URL}/registry/acme-all/v0.1/servers?search=data-analyzer"

# ============================================================================
# 9. DELETE — CLAIM VALIDATION
# ============================================================================
section "9. Delete — claim authorization"

# data-writer cannot delete platform team's entry (claims don't cover it)
check "data-writer cannot delete custom-linter (team mismatch)" 403 \
  -H "Authorization: Bearer ${TOKEN_DATA_WRITER}" \
  -X DELETE \
  "${BASE_URL}/v1/entries/server/com.acme%2Fcustom-linter/versions/1.0.0"

# platform-writer can delete their own entry
check "platform-writer deletes custom-linter" 204 \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_WRITER}" \
  -X DELETE \
  "${BASE_URL}/v1/entries/server/com.acme%2Fcustom-linter/versions/1.0.0"

# Super-admin can delete anyone's entry
check "super-admin deletes data-analyzer" 204 \
  -H "Authorization: Bearer ${TOKEN_SUPER}" \
  -X DELETE \
  "${BASE_URL}/v1/entries/server/com.acme%2Fdata-analyzer/versions/1.0.0"

# Re-deleting an already-deleted entry should return 404
check "delete already-deleted custom-linter returns 404" 404 \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_WRITER}" \
  -X DELETE \
  "${BASE_URL}/v1/entries/server/com.acme%2Fcustom-linter/versions/1.0.0"

# ============================================================================
# 10. CONFIG-MANAGED RESOURCE PROTECTION
# ============================================================================
section "10. Config-managed resource protection"

# Config-managed sources cannot be modified via API
check "platform-admin cannot delete config-managed source" 403 \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_ADMIN}" \
  -X DELETE \
  "${BASE_URL}/v1/sources/shared-catalog"

check "platform-admin cannot update config-managed source" 403 \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_ADMIN}" \
  -H "Content-Type: application/json" \
  -X PUT \
  -d '{"managed": {}, "claims": {"org": "acme"}}' \
  "${BASE_URL}/v1/sources/shared-catalog"

# Config-managed registries cannot be modified via API
check "platform-admin cannot delete config-managed registry" 403 \
  -H "Authorization: Bearer ${TOKEN_PLATFORM_ADMIN}" \
  -X DELETE \
  "${BASE_URL}/v1/registries/acme-all"

# ============================================================================
# SUMMARY
# ============================================================================

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
TOTAL=$((PASS + FAIL))
if [ "$FAIL" -eq 0 ]; then
  echo "  $(green "ALL ${TOTAL} TESTS PASSED")"
else
  echo "  $(green "${PASS} passed"), $(red "${FAIL} failed") out of ${TOTAL} tests"
fi
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

if [ "$FAIL" -gt 0 ]; then exit 1; else exit 0; fi
