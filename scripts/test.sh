#!/bin/bash

# PATCH /users/{id} test suite — run with the server up (make run)
# Expected codes are the CORRECT behavior: tests fail until the handler is finished.

set -u

BASE_URL="${BASE_URL:-localhost:8080}"
PASS=0
FAIL=0

check() {
    local desc="$1" expected="$2" path="$3" body="$4"
    local response status

    response=$(curl -s -w '\n%{http_code}' -X PATCH "$BASE_URL$path" \
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

echo "== Three-state contract on 'name' =="
check "key absent: nil pointer, omitempty skips"           200 /users/4    '{}'
check "explicit empty: pointer to \"\", min=5 fails"       400 /users/4    '{"name":""}'
check "valid value: rules run and pass"                    200 /users/4    '{"name":"Updated Name"}'

echo
echo "== Rejection branches =="
check "invalid email format"                               400 /users/4    '{"email":"not-an-email"}'
check "malformed JSON (truncated)"                         400 /users/4    '{"name":'
check "unknown user id"                                    404 /users/9999 '{"name":"Ghost User"}'
check "garbage id in path"                                 400 /users/abc  '{"name":"Valid Name Here"}'

echo
echo "$PASS passed, $FAIL failed"
[[ $FAIL -eq 0 ]]
