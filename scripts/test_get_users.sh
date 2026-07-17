#!/bin/bash

# GET /users test suite — run with the server up (make run)
# Three contract checks: 200, response is a JSON array (never null), and
# the no-leak rule: "password" must not appear anywhere in the body.

set -u

BASE_URL="${BASE_URL:-localhost:8080}"
PASS=0
FAIL=0

ok()   { echo "PASS  $1"; ((PASS++)); }
bad()  { echo "FAIL  $1"; ((FAIL++)); }

# GET /users is guarded by the auth middleware — create a disposable user
# and log in as it. Unique email per run so re-runs never hit the 409 branch.
email="get-test-$$-$RANDOM@example.com"
created=$(curl -s -X POST "$BASE_URL/users" \
    -H 'Content-Type: application/json' \
    -d "{\"name\":\"Disposable Get User\",\"email\":\"$email\",\"password\":\"password123\"}")
id=$(grep -o '"id":[0-9]*' <<<"$created" | cut -d: -f2)

if [[ -z "$id" ]]; then
    echo "SETUP FAILED: could not create disposable user. Response:"
    echo "$created"
    exit 1
fi

TOKEN=$(curl -s -X POST "$BASE_URL/auth/login" \
    -H 'Content-Type: application/json' \
    -d "{\"email\":\"$email\",\"password\":\"password123\"}" \
    | sed -n 's/.*"token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')

if [[ -z "$TOKEN" ]]; then
    echo "SETUP FAILED: could not log in as the disposable user."
    exit 1
fi
echo "setup: created disposable user id=$id, logged in"
echo

response=$(curl -s -w '\n%{http_code}' \
    -H "Authorization: Bearer $TOKEN" "$BASE_URL/users")
status=$(tail -n1 <<<"$response")
body=$(sed '$d' <<<"$response")

if [[ "$status" == "200" ]]; then
    ok "[200] list endpoint responds"
else
    bad "[want 200, got $status] list endpoint responds"
fi

if [[ "${body:0:1}" == "[" ]]; then
    ok "body is a JSON array (never null)"
else
    bad "body is not an array — starts with '${body:0:1}'"
fi

if grep -qi '"password"' <<<"$body"; then
    bad "LEAK: \"password\" field present in list response"
else
    ok "no password field anywhere in the body"
fi

# Cleanup: delete the disposable user.
status=$(curl -s -o /dev/null -w '%{http_code}' \
    -H "Authorization: Bearer $TOKEN" \
    -X DELETE "$BASE_URL/users/$id")

if [[ "$status" == "204" ]]; then
    ok "[204] cleanup: disposable user deleted"
else
    bad "[want 204, got $status] cleanup — user $id may be left behind"
fi

echo
echo "$PASS passed, $FAIL failed"
[[ $FAIL -eq 0 ]]
