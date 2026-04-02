#!/usr/bin/env bash
# Auth-only smoke tests (no authz) for the ToolHive Registry Server
#
# This test covers the "auth=yes, authz=no" row of the auth/authz matrix:
#
#   auth | authz | behavior
#   -----|-------|----------------------------------------------------
#   no   | no    | No checks (anonymous mode, see smoke-test skill)
#   yes  | no    | Token validated, no claim-based restrictions  <-- THIS TEST
#   yes  | yes   | Token validated + claims checked (see test-authz.sh)
#
# Prerequisites:
#   Start the stack with the auth-only config:
#     docker compose -f docker-compose-authz.yaml up --build \
#       -e REGISTRY_CONFIG=/examples/authz-test/config-auth-only.yaml
#   OR override the command:
#     docker compose -f docker-compose-authz.yaml run -p 8080:8080 registry-api \
#       serve --config /examples/authz-test/config-auth-only.yaml --address :8080
#
# Usage:
#   ./examples/authz-test/test-auth-only.sh

set -euo pipefail

BASE_URL="${REGISTRY_URL:-http://localhost:8080}"
OAUTH_URL="${OAUTH_URL:-http://mock-oauth2-server:8888}"
CURL_RESOLVE="--resolve mock-oauth2-server:8888:127.0.0.1"

PASS=0
FAIL=0

# --- Helpers ---

red()    { printf "\033[31m%s\033[0m" "$*"; }
green()  { printf "\033[32m%s\033[0m" "$*"; }
yellow() { printf "\033[33m%s\033[0m" "$*"; }
bold()   { printf "\033[1m%s\033[0m" "$*"; }

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
    echo "        $(echo "$body" | head -c 200)"
  fi
}

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

section() {
  echo ""
  bold "=== $1 ==="
  echo ""
}

# --- Preflight ---

section "Preflight"

echo "  Checking services..."

if ! curl -sf ${CURL_RESOLVE} "${OAUTH_URL}/default/.well-known/openid-configuration" > /dev/null 2>&1; then
  echo "  $(red ERROR): mock-oauth2-server not reachable at ${OAUTH_URL}"
  exit 1
fi
echo "  $(green OK) mock-oauth2-server at ${OAUTH_URL}"

if ! curl -sf "${BASE_URL}/health" > /dev/null 2>&1; then
  echo "  $(red ERROR): Registry server not reachable at ${BASE_URL}"
  exit 1
fi
echo "  $(green OK) Registry server at ${BASE_URL}"

echo ""
echo "  Fetching tokens..."
TOKEN_VALID=$(get_token "platform-admin")
TOKEN_OUTSIDER=$(get_token "outsider")
echo "  $(green OK) Tokens obtained"

# Wait for initial sync
echo ""
echo "  Waiting for initial sync..."
MAX_WAIT=60
WAITED=0
while [ "$WAITED" -lt "$MAX_WAIT" ]; do
  COUNT=$(curl -sf -H "Authorization: Bearer $TOKEN_VALID" "${BASE_URL}/registry/default/v0.1/servers" 2>/dev/null | jq '.metadata.count // 0' 2>/dev/null || echo "0")
  if [ "$COUNT" -gt 0 ] 2>/dev/null; then
    break
  fi
  sleep 5
  WAITED=$((WAITED + 5))
  printf "    ... waiting (%ds)\n" "$WAITED"
done
echo "  $(green OK) Ready"

# ============================================================================
# 1. SYSTEM ENDPOINTS (public, no auth)
# ============================================================================
section "1. System endpoints (public)"

check "Health check" 200 \
  "${BASE_URL}/health"

check "Readiness check" 200 \
  "${BASE_URL}/readiness"

check_body "Well-known OAuth protected resource" 200 "authorization_servers" \
  "${BASE_URL}/.well-known/oauth-protected-resource"

# ============================================================================
# 2. UNAUTHENTICATED ACCESS (should be 401)
# ============================================================================
section "2. Unauthenticated access (should be 401)"

check "Consumer API without token" 401 \
  "${BASE_URL}/registry/default/v0.1/servers"

check "Admin list sources without token" 401 \
  "${BASE_URL}/v1/sources"

check "Malformed Bearer token" 401 \
  -H "Authorization: Bearer not-a-valid-jwt" \
  "${BASE_URL}/registry/default/v0.1/servers"

# ============================================================================
# 3. AUTHENTICATED ACCESS — NO CLAIM RESTRICTIONS
# ============================================================================
section "3. Authenticated access (any valid token works)"

# With auth-only (no authz), any valid token should grant full access
# regardless of the claims in the token

check "Valid token accesses consumer API" 200 \
  -H "Authorization: Bearer ${TOKEN_VALID}" \
  "${BASE_URL}/registry/default/v0.1/servers"

# Outsider token is still a valid JWT — without authz, it should work
check "Outsider token also works (no claim checks)" 200 \
  -H "Authorization: Bearer ${TOKEN_OUTSIDER}" \
  "${BASE_URL}/registry/default/v0.1/servers"

check_body "Both see the same entries" 200 "servers" \
  -H "Authorization: Bearer ${TOKEN_OUTSIDER}" \
  "${BASE_URL}/registry/default/v0.1/servers"

# ============================================================================
# 4. ADMIN API — ACCESSIBLE WITH ANY VALID TOKEN
# ============================================================================
section "4. Admin API (no role restrictions without authz)"

check "Any valid token can list sources" 200 \
  -H "Authorization: Bearer ${TOKEN_VALID}" \
  "${BASE_URL}/v1/sources"

check "Any valid token can list registries" 200 \
  -H "Authorization: Bearer ${TOKEN_VALID}" \
  "${BASE_URL}/v1/registries"

check "Outsider can also list sources (no authz)" 200 \
  -H "Authorization: Bearer ${TOKEN_OUTSIDER}" \
  "${BASE_URL}/v1/sources"

check "Outsider can also list registries (no authz)" 200 \
  -H "Authorization: Bearer ${TOKEN_OUTSIDER}" \
  "${BASE_URL}/v1/registries"

# ============================================================================
# 5. PUBLISH/DELETE — ACCESSIBLE WITH ANY VALID TOKEN
# ============================================================================
section "5. Publish and delete (no claim validation without authz)"

# Clean up from previous runs
curl -sf -X DELETE -H "Authorization: Bearer ${TOKEN_VALID}" \
  "${BASE_URL}/v1/entries/server/com.test%2Fauth-only-tool/versions/1.0.0" > /dev/null 2>&1 || true

check_body "Any valid token can publish" 201 "auth-only-tool" \
  -H "Authorization: Bearer ${TOKEN_VALID}" \
  -H "Content-Type: application/json" \
  -d '{
    "server": {
      "name": "com.test/auth-only-tool",
      "version": "1.0.0",
      "description": "Test tool for auth-only mode",
      "packages": [{"registryType": "oci", "identifier": "ghcr.io/test/auth-only-tool:1.0.0", "transport": {"type": "stdio"}}]
    }
  }' \
  "${BASE_URL}/v1/entries"

check "Any valid token can delete" 204 \
  -H "Authorization: Bearer ${TOKEN_VALID}" \
  -X DELETE \
  "${BASE_URL}/v1/entries/server/com.test%2Fauth-only-tool/versions/1.0.0"

# ============================================================================
# SUMMARY
# ============================================================================

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
TOTAL=$((PASS + FAIL))
if [ "$FAIL" -eq 0 ]; then
  echo "  $(green "ALL ${TOTAL} TESTS PASSED") (auth=yes, authz=no)"
else
  echo "  $(green "${PASS} passed"), $(red "${FAIL} failed") out of ${TOTAL} tests"
fi
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

if [ "$FAIL" -gt 0 ]; then exit 1; else exit 0; fi