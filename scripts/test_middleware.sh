#!/bin/bash

# Auth middleware test suite — run with the server up (make run)
# Self-contained: registers a disposable user (proves POST /users is
# public), logs in (proves /auth/login is public), then checks the guard
# on the protected routes: 401 for missing/malformed/garbage/tampered
# tokens, WWW-Authenticate on every 401, 200 with the real token, and a
# token-authorized DELETE as cleanup.
# NOT covered: expired token (lifetime is 15m; would need a frozen token
# or a config override to test live).

set -u

BASE_URL="${BASE_URL:-localhost:8080}"
PASS=0
FAIL=0

ok()   { echo "PASS  $1"; ((PASS++)); }
bad()  { echo "FAIL  $1"; ((FAIL++)); }

# Unique email per run so re-runs never hit the 409 duplicate branch.
EMAIL="mw-test-$$-$RANDOM@example.com"
PASSWORD="s3cret-passw0rd"

# ---------- 1. Setup: register (public route, no token needed) ----------
response=$(curl -s -w '\n%{http_code}' \
  -X POST "$BASE_URL/users" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"Disposable MW User\",\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}")
status=$(tail -n1 <<<"$response")
body=$(sed '$d' <<<"$response")

if [[ "$status" == "201" ]]; then
    ok "[201] POST /users works without a token (public)"
else
    echo "SETUP FAILED: register returned $status (want 201). Response:"
    echo "$body"
    exit 1
fi

USER_ID=$(sed -n 's/.*"id"[[:space:]]*:[[:space:]]*\([0-9]\+\).*/\1/p' <<<"$body")

if [[ -z "$USER_ID" ]]; then
    echo "SETUP FAILED: no id in register response — cleanup impossible. Response:"
    echo "$body"
    exit 1
fi

# ---------- 2. Setup: login (public route) ----------
response=$(curl -s -w '\n%{http_code}' \
  -X POST "$BASE_URL/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}")
status=$(tail -n1 <<<"$response")
body=$(sed '$d' <<<"$response")

if [[ "$status" == "200" ]]; then
    ok "[200] POST /auth/login works without a token (public)"
else
    echo "SETUP FAILED: login returned $status (want 200). Response:"
    echo "$body"
    exit 1
fi

TOKEN=$(sed -n 's/.*"token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' <<<"$body")

if [[ -z "$TOKEN" ]]; then
    echo "SETUP FAILED: login response has no token. Response:"
    echo "$body"
    exit 1
fi

# ---------- 3. No Authorization header: 401 + WWW-Authenticate ----------
response=$(curl -s -D - -o /dev/null -w '%{http_code}' \
  "$BASE_URL/users")
status=$(tail -n1 <<<"$response")
headers=$(sed '$d' <<<"$response")

if [[ "$status" == "401" ]]; then
    ok "[401] GET /users with no token is rejected"
else
    bad "[want 401, got $status] GET /users with no token is rejected"
fi

if grep -qi 'www-authenticate' <<<"$headers"; then
    ok "401 carries the WWW-Authenticate header"
else
    bad "401 is missing the WWW-Authenticate header"
fi

# ---------- 4. Malformed header: scheme is not Bearer ----------
status=$(curl -s -o /dev/null -w '%{http_code}' \
  -H "Authorization: Basic $TOKEN" \
  "$BASE_URL/users")

if [[ "$status" == "401" ]]; then
    ok "[401] non-Bearer scheme is rejected"
else
    bad "[want 401, got $status] non-Bearer scheme is rejected"
fi

# ---------- 5. Malformed header: bare token, no scheme ----------
status=$(curl -s -o /dev/null -w '%{http_code}' \
  -H "Authorization: $TOKEN" \
  "$BASE_URL/users")

if [[ "$status" == "401" ]]; then
    ok "[401] bare token without 'Bearer ' is rejected"
else
    bad "[want 401, got $status] bare token without 'Bearer ' is rejected"
fi

# ---------- 6. Garbage token ----------
status=$(curl -s -o /dev/null -w '%{http_code}' \
  -H "Authorization: Bearer not.a.token" \
  "$BASE_URL/users")

if [[ "$status" == "401" ]]; then
    ok "[401] garbage token is rejected"
else
    bad "[want 401, got $status] garbage token is rejected"
fi

# ---------- 7. Tampered token: real token, signature broken ----------
# Chop the last 4 chars of the signature — payload intact, signature wrong.
status=$(curl -s -o /dev/null -w '%{http_code}' \
  -H "Authorization: Bearer ${TOKEN%????}" \
  "$BASE_URL/users")

if [[ "$status" == "401" ]]; then
    ok "[401] tampered signature is rejected"
else
    bad "[want 401, got $status] tampered signature is rejected"
fi

# ---------- 8. Valid token: list ----------
status=$(curl -s -o /dev/null -w '%{http_code}' \
  -H "Authorization: Bearer $TOKEN" \
  "$BASE_URL/users")

if [[ "$status" == "200" ]]; then
    ok "[200] GET /users with a valid token"
else
    bad "[want 200, got $status] GET /users with a valid token"
fi

# ---------- 9. Valid token: single resource ----------
status=$(curl -s -o /dev/null -w '%{http_code}' \
  -H "Authorization: Bearer $TOKEN" \
  "$BASE_URL/users/$USER_ID")

if [[ "$status" == "200" ]]; then
    ok "[200] GET /users/{id} with a valid token"
else
    bad "[want 200, got $status] GET /users/{id} with a valid token"
fi

# ---------- 10. DELETE without token is blocked ----------
status=$(curl -s -o /dev/null -w '%{http_code}' \
  -X DELETE "$BASE_URL/users/$USER_ID")

if [[ "$status" == "401" ]]; then
    ok "[401] DELETE without a token is rejected"
else
    bad "[want 401, got $status] DELETE without a token is rejected"
fi

# ---------- 11. Cleanup: DELETE with the token ----------
status=$(curl -s -o /dev/null -w '%{http_code}' \
  -H "Authorization: Bearer $TOKEN" \
  -X DELETE "$BASE_URL/users/$USER_ID")

if [[ "$status" == "204" ]]; then
    ok "[204] cleanup: disposable user deleted (token authorized)"
else
    bad "[want 204, got $status] cleanup — user $USER_ID may be left behind"
fi

echo
echo "$PASS passed, $FAIL failed"
[[ $FAIL -eq 0 ]]
