#!/bin/bash
set -euo pipefail

# Ensure compose is up
make dev-up || true

export DOMAIN="localtest.me"
export FORGE_DATABASE_URL="postgres://forge:password@localhost:5432/forge?sslmode=disable"
export FORGE_NATS_URL="nats://localhost:4222"
export FORGE_S3_ENDPOINT="localhost:9000"
export FORGE_S3_BUCKET="forge-snapshots"
export FORGE_S3_ACCESS_KEY="minioadmin"
export FORGE_S3_SECRET_KEY="minioadmin"
export FORGE_S3_REGION="us-east-1"
export FORGE_INTERNAL_TOKEN="devtoken"
export FORGE_COOKIE_SECRET="devcookie"
export FORGE_TLS="off"
export FORGE_RUNTIME="runc"
export FORGE_FAKE_LLM="1"
export FORGE_SIGNUP_GRANT_CREDITS="50"
export FORGE_MAX_PROJECTS_PER_USER="20"
export FORGE_ADMIN_EMAILS="admin@example.com"
export FORGE_METRICS_ADDR="localhost:9091"

# Build all binaries
make build

# Start forged in background
bin/forged-amd64 &
FORGED_PID=$!
sleep 2

# Helper to check JSON error codes
check_error() {
    local response="$1"
    local expected_code="$2"
    if ! echo "$response" | grep -q "\"code\":\"$expected_code\""; then
        echo "FAIL: expected error code $expected_code, got $response"
        exit 1
    fi
}

echo "Testing signup..."
SIGNUP_RES=$(curl -s -X POST http://localhost:8080/v1/auth/signup -c cookie.txt \
    -H "Content-Type: application/json" \
    -d '{"email":"user1@example.com", "password":"password123"}')
if ! echo "$SIGNUP_RES" | grep -q "user1@example.com"; then
    echo "FAIL: Signup failed: $SIGNUP_RES"
    exit 1
fi
echo "PASS: signup successful"

# Ensure 50 credits (50000000 microcredits)
if ! echo "$SIGNUP_RES" | grep -q '"balance_microcredits":50000000'; then
    echo "FAIL: Signup grant incorrect"
    exit 1
fi
echo "PASS: signup grant applied"

echo "Testing ledger..."
LEDGER_RES=$(curl -s http://localhost:8080/v1/credits/ledger -b cookie.txt)
if ! echo "$LEDGER_RES" | grep -q "signup_grant"; then
    echo "FAIL: ledger does not contain signup_grant"
    exit 1
fi
echo "PASS: ledger row signup_grant present"

echo "Testing CSRF..."
CREATE_RES_NO_CSRF=$(curl -s -X POST http://localhost:8080/v1/projects -b cookie.txt \
    -H "Content-Type: application/json" \
    -d '{"name":"test", "template":"static"}')
check_error "$CREATE_RES_NO_CSRF" "forbidden"
echo "PASS: mutation without CSRF blocked"

echo "Testing project creation..."
CREATE_RES=$(curl -s -X POST http://localhost:8080/v1/projects -b cookie.txt \
    -H "X-Forge-CSRF: 1" \
    -H "Content-Type: application/json" \
    -d '{"name":"test", "template":"static"}')
PROJ_ID=$(echo "$CREATE_RES" | grep -o '"id":"[^"]*' | cut -d'"' -f4 | head -1)
echo "PASS: project created"

echo "Testing 21st project limit..."
for i in {2..20}; do
    curl -s -X POST http://localhost:8080/v1/projects -b cookie.txt -H "X-Forge-CSRF: 1" -H "Content-Type: application/json" -d "{\"name\":\"test$i\", \"template\":\"static\"}" >/dev/null
done
FAIL_CREATE_RES=$(curl -s -X POST http://localhost:8080/v1/projects -b cookie.txt -H "X-Forge-CSRF: 1" -H "Content-Type: application/json" -d '{"name":"test21", "template":"static"}')
check_error "$FAIL_CREATE_RES" "validation_failed"
echo "PASS: 21st project -> validation_failed"

echo "Testing user B fetching user A's project..."
curl -s -X POST http://localhost:8080/v1/auth/signup -c cookie_b.txt \
    -H "Content-Type: application/json" \
    -d '{"email":"user2@example.com", "password":"password123"}' >/dev/null
FETCH_RES=$(curl -s http://localhost:8080/v1/projects/$PROJ_ID -b cookie_b.txt)
check_error "$FETCH_RES" "not_found"
echo "PASS: user B fetching user A's project -> 404"

echo "Testing admin features..."
# Signup admin
curl -s -X POST http://localhost:8080/v1/auth/signup -c cookie_admin.txt \
    -H "Content-Type: application/json" \
    -d '{"email":"admin@example.com", "password":"password123"}' >/dev/null
USER_A_ID=$(echo "$SIGNUP_RES" | grep -o '"id":"[^"]*' | cut -d'"' -f4 | head -1)

# Admin grants credits
curl -s -X POST http://localhost:8080/v1/admin/users/$USER_A_ID/grant -b cookie_admin.txt \
    -H "X-Forge-CSRF: 1" -H "Content-Type: application/json" \
    -d '{"credits":10}' >/dev/null

# Fetch user A ledger to verify
LEDGER_RES_A=$(curl -s http://localhost:8080/v1/credits/ledger -b cookie.txt)
if ! echo "$LEDGER_RES_A" | grep -q "admin_grant"; then
    echo "FAIL: admin grant not reflected"
    exit 1
fi
echo "PASS: admin grant reflected in balance"

# Suspend user A
curl -s -X POST http://localhost:8080/v1/admin/users/$USER_A_ID/suspend -b cookie_admin.txt \
    -H "X-Forge-CSRF: 1" >/dev/null

# Try login as suspended user
LOGIN_RES=$(curl -s -X POST http://localhost:8080/v1/auth/login \
    -H "Content-Type: application/json" \
    -d '{"email":"user1@example.com", "password":"password123"}')
check_error "$LOGIN_RES" "forbidden"
echo "PASS: suspended user login -> 403"

# Try mutation on existing session
SUSPEND_MUTATE_RES=$(curl -s -X POST http://localhost:8080/v1/projects -b cookie.txt \
    -H "X-Forge-CSRF: 1" -H "Content-Type: application/json" \
    -d '{"name":"test22", "template":"static"}')
check_error "$SUSPEND_MUTATE_RES" "forbidden"
echo "PASS: suspended user existing session mutations -> 403"

# Rate limit testing...
echo "Testing login rate limit..."
for i in {1..10}; do
    curl -s -X POST http://localhost:8080/v1/auth/login -H "Content-Type: application/json" -d '{"email":"fake@example.com", "password":"password123"}' >/dev/null
done
RL_RES=$(curl -s -X POST http://localhost:8080/v1/auth/login -H "Content-Type: application/json" -d '{"email":"fake@example.com", "password":"password123"}')
check_error "$RL_RES" "rate_limited"
echo "PASS: login rate limit trips"

# Stop server and re-start with closed signups
kill $FORGED_PID
wait $FORGED_PID || true

export FORGE_SIGNUPS="closed"
bin/forged-amd64 &
FORGED_PID=$!
sleep 2

CLOSED_SIGNUP_RES=$(curl -s -X POST http://localhost:8080/v1/auth/signup \
    -H "Content-Type: application/json" \
    -d '{"email":"user3@example.com", "password":"password123"}')
check_error "$CLOSED_SIGNUP_RES" "forbidden"
echo "PASS: FORGE_SIGNUPS=closed blocks signup"

kill $FORGED_PID
wait $FORGED_PID || true

echo "All M1 checks passed."
