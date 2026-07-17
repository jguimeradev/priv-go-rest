#!/bin/bash

# DELETE /users/{id} test suite — run with the server up (make run)
# Self-contained: creates a disposable user via POST, then runs the delete
# lifecycle against it — never touches the seed data the PATCH suite uses.
# The delete → get → delete-again triple proves: success, state change, idempotency.

set -u

BASE_URL="${BASE_URL:-localhost:8080}"
PASS=0
FAIL=0

check() {
    local desc="$1" expected="$2" method="$3" path="$4"
    local response status

    response=$(curl -s -w '\n%{http_code}' -X "$method" \
        -H "Authorization: Bearer $TOKEN" "$BASE_URL$path")
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
email="delete-test-$$-$RANDOM@example.com"
created=$(curl -s -X POST "$BASE_URL/users" \
    -H 'Content-Type: application/json' \
    -d "{\"name\":\"Disposable User\",\"email\":\"$email\",\"password\":\"password123\"}")
id=$(grep -o '"id":[0-9]*' <<<"$created" | cut -d: -f2)

if [[ -z "$id" ]]; then
    echo "SETUP FAILED: could not create disposable user. Response:"
    echo "$created"
    exit 1
fi
echo "setup: created disposable user id=$id"

# DELETE and GET are guarded by the auth middleware — log in as the disposable user.
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
echo "== Rejection branches =="
check "garbage id in path"                      400 DELETE /users/abc
check "unknown user id"                         404 DELETE /users/999999

echo
echo "== Delete lifecycle on user $id =="
check "delete existing user"                    204 DELETE "/users/$id"
check "GET after delete: state really changed"  404 GET    "/users/$id"
check "delete again: idempotent, 404 informs"   404 DELETE "/users/$id"

echo
echo "$PASS passed, $FAIL failed"
[[ $FAIL -eq 0 ]]
