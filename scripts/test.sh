#!/bin/bash

# PATCH /users/{id} test suite — run with the server up (make run)
# Self-contained: creates a disposable user via POST and targets it,
# so the suite never depends on seed data surviving (user 4 incident, 2026-07-06).

set -u

BASE_URL="${BASE_URL:-localhost:8080}"
PASS=0
FAIL=0

check() {
    local desc="$1" expected="$2" path="$3" body="$4"
    local response status

    response=$(curl -s -w '\n%{http_code}' -X PATCH "$BASE_URL$path" \
        -H "Authorization: Bearer $TOKEN" \
        -H 'Content-Type: application/json' -d "$body")
    status=$(tail -n1 <<<"$response")

    if [[ "$status" == "$expected" ]]; then
        echo "PASS  [$expected] $desc"
        ((PASS++))
    else
        echo "FAIL  [want $expected, got $status] $desc"
        sed '$d' <<<"$response" | sed 's/^/      | /'
        ((FAIL++))
    fi
}

# Setup: unique email per run so re-runs never hit the 409 duplicate branch.
email="patch-test-$$-$RANDOM@example.com"
created=$(curl -s -X POST "$BASE_URL/users" \
    -H 'Content-Type: application/json' \
    -d "{\"name\":\"Disposable Patch Target\",\"email\":\"$email\",\"password\":\"password123\"}")
id=$(grep -o '"id":[0-9]*' <<<"$created" | cut -d: -f2)

if [[ -z "$id" ]]; then
    echo "SETUP FAILED: could not create disposable user. Response:"
    echo "$created"
    exit 1
fi
echo "setup: created disposable user id=$id"

# PATCH is guarded by the auth middleware — log in as the disposable user.
TOKEN=$(curl -s -X POST "$BASE_URL/auth/login" \
    -H 'Content-Type: application/json' \
    -d "{\"email\":\"$email\",\"password\":\"password123\"}" \
    | sed -n 's/.*"token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')

if [[ -z "$TOKEN" ]]; then
    echo "SETUP FAILED: could not log in as the disposable user."
    exit 1
fi
echo "setup: logged in, token acquired"

echo
echo "== Three-state contract on 'name' =="
check "key absent: nil pointer, omitempty skips"           200 "/users/$id"  '{}'
check "explicit empty: pointer to \"\", min=5 fails"       400 "/users/$id"  '{"name":""}'
check "valid value: rules run and pass"                    200 "/users/$id"  '{"name":"Updated Name"}'

echo
echo "== Rejection branches =="
check "invalid email format"                               400 "/users/$id"  '{"email":"not-an-email"}'
check "malformed JSON (truncated)"                         400 "/users/$id"  '{"name":'
check "unknown user id"                                    404 /users/999999 '{"name":"Ghost User"}'
check "garbage id in path"                                 400 /users/abc    '{"name":"Valid Name Here"}'

echo
echo "$PASS passed, $FAIL failed"
[[ $FAIL -eq 0 ]]





