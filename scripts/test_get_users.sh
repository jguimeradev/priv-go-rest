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

response=$(curl -s -w '\n%{http_code}' "$BASE_URL/users")
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

echo
echo "$PASS passed, $FAIL failed"
[[ $FAIL -eq 0 ]]
