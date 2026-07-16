#!/bin/bash

# POST /auth/login test suite — run with the server up (make run)
# Self-contained: registers a disposable user, logs in as it, checks the
# token shape and the failure contract (401 wrong password, the IDENTICAL
# 401 for an unknown email, 400 missing field, WWW-Authenticate header),
# then deletes the user. Aborts if setup fails — every later check
# depends on that user.

set -u

BASE_URL="${BASE_URL:-localhost:8080}"
PASS=0
FAIL=0

ok()   { echo "PASS  $1"; ((PASS++)); }
bad()  { echo "FAIL  $1"; ((FAIL++)); }

# Unique email per run so re-runs never hit the 409 duplicate branch.
EMAIL="login-test-$$-$RANDOM@example.com"
PASSWORD="s3cret-passw0rd"

# ---------- 1. Setup: POST /users (register) ----------
response=$(curl -s -w '\n%{http_code}' \
  -X POST "$BASE_URL/users" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"Disposable Login User\",\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}")
status=$(tail -n1 <<<"$response")
body=$(sed '$d' <<<"$response")

if [[ "$status" == "201" ]]; then
    ok "[201] register creates the disposable user"
else
    echo "SETUP FAILED: register returned $status (want 201). Response:"
    echo "$body"
    exit 1
fi

if grep -qi '"password"' <<<"$body"; then
    bad "LEAK: \"password\" field present in register response"
else
    ok "no password field in register response"
fi

# id is a bare number in the body (no quotes) — needed for cleanup.
USER_ID=$(sed -n 's/.*"id"[[:space:]]*:[[:space:]]*\([0-9]\+\).*/\1/p' <<<"$body")

if [[ -z "$USER_ID" ]]; then
    echo "SETUP FAILED: no id in register response — cleanup impossible. Response:"
    echo "$body"
    exit 1
fi

# ---------- 2. Good login ----------
response=$(curl -s -w '\n%{http_code}' \
  -X POST "$BASE_URL/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}")
status=$(tail -n1 <<<"$response")
body=$(sed '$d' <<<"$response")

if [[ "$status" == "200" ]]; then
    ok "[200] login endpoint responds"
else
    bad "[want 200, got $status] login endpoint responds — $body"
fi

TOKEN=$(sed -n 's/.*"token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' <<<"$body")

if [[ -n "$TOKEN" ]]; then
    ok "login returns a token field"
else
    bad "login response has no token field"
fi

# JWT shape: three base64url segments
if [[ "$TOKEN" =~ ^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$ ]]; then
    ok "token is a well-formed JWT (header.payload.signature)"
else
    bad "token is not a well-formed JWT — '${TOKEN:0:20}...'"
fi

# ---------- 3. Wrong password: 401 + WWW-Authenticate ----------
# -D - dumps headers to stdout, body discarded; status still appended last.
response=$(curl -s -D - -o /dev/null -w '%{http_code}' \
  -X POST "$BASE_URL/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"wrong-password\"}")
status=$(tail -n1 <<<"$response")
headers=$(sed '$d' <<<"$response")

if [[ "$status" == "401" ]]; then
    ok "[401] wrong password is rejected"
else
    bad "[want 401, got $status] wrong password is rejected"
fi

if grep -qi 'www-authenticate' <<<"$headers"; then
    ok "401 carries the WWW-Authenticate header"
else
    bad "401 is missing the WWW-Authenticate header"
fi

# ---------- 4. Unknown email: the IDENTICAL 401 (no user enumeration) ----------
status=$(curl -s -o /dev/null -w '%{http_code}' \
  -X POST "$BASE_URL/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"nobody-$$-$RANDOM@example.com\",\"password\":\"whatever\"}")

if [[ "$status" == "401" ]]; then
    ok "[401] unknown email answers the same as wrong password"
else
    bad "[want 401, got $status] unknown email answers the same as wrong password"
fi

# ---------- 5. Missing field: 400 ----------
status=$(curl -s -o /dev/null -w '%{http_code}' \
  -X POST "$BASE_URL/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\"}")

if [[ "$status" == "400" ]]; then
    ok "[400] missing password field is rejected"
else
    bad "[want 400, got $status] missing password field is rejected"
fi

# ---------- 6. Cleanup: DELETE the disposable user ----------
status=$(curl -s -o /dev/null -w '%{http_code}' \
  -X DELETE "$BASE_URL/users/$USER_ID")

if [[ "$status" == "204" ]]; then
    ok "[204] cleanup: disposable user deleted"
else
    bad "[want 204, got $status] cleanup: disposable user deleted — user $USER_ID may be left behind"
fi

echo
echo "$PASS passed, $FAIL failed"
[[ $FAIL -eq 0 ]]
