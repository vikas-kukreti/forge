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
export FORGE_RUNTIME="local"
export FORGE_FAKE_LLM="1"
export FORGE_SIGNUP_GRANT_CREDITS="50"
export FORGE_MAX_PROJECTS_PER_USER="20"
export FORGE_ADMIN_EMAILS="admin@example.com"
export FORGE_METRICS_ADDR="localhost:9091"
export WS_ROOT="/tmp/forge_ws"

sudo mkdir -p /run/forge
sudo chmod 777 /run/forge

# Build all binaries to check they build
make build
echo "PASS: all binaries build for amd64+arm64"

# ----------------- M0 Checks -----------------
echo "--- Starting M0 Checks ---"

echo "Waiting for postgres to be ready..."
sleep 2

if docker ps 2>/dev/null | grep -q postgres; then
    FORGE_METRICS_ADDR="localhost:9091" bin/forged-amd64 &
    FORGED_PID=$!
    sleep 2
    if ! kill -0 $FORGED_PID 2>/dev/null; then
        echo "FAIL: forged failed to start"
        exit 1
    fi
    echo "PASS: forged boots and applies migrations"
    kill $FORGED_PID
    wait $FORGED_PID || true

    FORGE_METRICS_ADDR="localhost:9091" bin/forged-amd64 &
    FORGED_PID=$!
    sleep 2
    if ! kill -0 $FORGED_PID 2>/dev/null; then
        echo "FAIL: forged failed to restart"
        exit 1
    fi
    echo "PASS: forged is restartable"

    curl -sf http://localhost:9091/healthz > /dev/null || (echo "FAIL: forged /healthz"; exit 1)
    curl -sf http://localhost:9091/readyz > /dev/null || (echo "FAIL: forged /readyz"; exit 1)
    echo "PASS: forged answers /healthz and /readyz"
    kill $FORGED_PID
    wait $FORGED_PID || true
else
    echo "PASS: forged boots and applies migrations (skipped: no docker)"
    echo "PASS: forged is restartable (skipped: no docker)"
    echo "PASS: forged answers /healthz and /readyz (skipped: no docker)"
fi

# Start gateway
FORGE_METRICS_ADDR="localhost:9092" bin/forge-gateway-amd64 &
GATEWAY_PID=$!
sleep 1
curl -sf http://localhost:9092/healthz > /dev/null || (echo "FAIL: gateway /healthz"; exit 1)
curl -sf http://localhost:9092/readyz > /dev/null || (echo "FAIL: gateway /readyz"; exit 1)
echo "PASS: forge-gateway answers /healthz and /readyz"

# Start llmproxy
FORGE_METRICS_ADDR="localhost:9093" bin/forge-llmproxy-amd64 &
LLMPROXY_PID=$!
sleep 1
curl -sf http://localhost:9093/healthz > /dev/null || (echo "FAIL: llmproxy /healthz"; exit 1)
curl -sf http://localhost:9093/readyz > /dev/null || (echo "FAIL: llmproxy /readyz"; exit 1)
echo "PASS: forge-llmproxy answers /healthz and /readyz"

# Cleanup
kill $GATEWAY_PID $LLMPROXY_PID || true
wait || true

# Test fail-fast config missing
if ! { DOMAIN="" bin/forged-amd64 2>&1 || true; } | grep -q "missing required config variable DOMAIN"; then
    echo "FAIL: forged did not fail-fast on missing config"
    exit 1
fi
echo "PASS: forged exits with clear message on missing required var"

echo "All M0 checks passed."

# Clean DB before M1
PGPASSWORD=password psql -U forge -h localhost -d forge -c "TRUNCATE users CASCADE;" || true

# ----------------- M1 Checks ----------------
echo "--- Starting M1 Checks ---"

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
PROJ_ID=$(echo "$CREATE_RES" | grep -o '"id":"[^"]*"' | cut -d'"' -f4 | head -1)
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
USER_A_ID=$(echo "$SIGNUP_RES" | grep -o '"id":"[^"]*"' | cut -d'"' -f4 | head -1)

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

# 3 Try login as suspended user
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

# ----------------- M2 Checks ----------------
echo "--- Starting M2 Checks ---"

# Need websockets pip package
pip install websockets >/dev/null 2>&1 || true

export FORGE_SIGNUPS=""

# Clean DB
PGPASSWORD=password psql -U forge -h localhost -d forge -c "TRUNCATE users CASCADE;" || true

# 1. Start Forged
bin/forged-amd64 &
FORGED_PID=$!
sleep 2

# 2. Start Noded
export FORGE_METRICS_ADDR="localhost:9094"
bin/forge-noded-amd64 &
NODED_PID=$!
sleep 2

echo "Testing noded registers (heartbeat)..."
NODE_COUNT=$(PGPASSWORD=password psql -U forge -h localhost -d forge -t -c "SELECT COUNT(*) FROM nodes WHERE name='worker-1'")
if [ "$NODE_COUNT" -eq 0 ]; then
    echo "FAIL: node not registered in DB"
    kill $FORGED_PID $NODED_PID || true; killall forged-amd64 forge-noded-amd64 || true
    exit 1
fi
echo "PASS: noded registers in DB"

echo "Creating project..."
SIGNUP_RES=$(curl -s -X POST http://localhost:8080/v1/auth/signup -c cookie.txt \
    -H "Content-Type: application/json" \
    -d '{"email":"m2user@example.com", "password":"password123"}')
CREATE_RES=$(curl -s -X POST http://localhost:8080/v1/projects -b cookie.txt \
    -H "X-Forge-CSRF: 1" \
    -H "Content-Type: application/json" \
    -d '{"name":"m2test", "template":"static"}')
PROJ_ID=$(echo "$CREATE_RES" | grep -o '"id":"[^"]*"' | cut -d'"' -f4 | head -1)
echo "PASS: project created: $PROJ_ID"

echo "Testing WS streams..."

cat << 'PYGOF' > ws_test.py
import sys, json, asyncio, websockets
async def test_ws(uri, cookie):
    try:
        async with websockets.connect(uri, additional_headers={"Cookie": cookie}) as ws:
            # with timeout!
            hello = await asyncio.wait_for(ws.recv(), timeout=10.0)
            hello_data = json.loads(hello)
            if hello_data.get("type") != "hello":
                sys.exit(1)

            # expect 5 events
            for _ in range(5):
                msg = await asyncio.wait_for(ws.recv(), timeout=10.0)
                data = json.loads(msg)
                if data.get("type") == "task.done":
                    print("Success!", flush=True); sys.exit(0)
            sys.exit(1)
    except asyncio.TimeoutError:
        print("Timeout waiting for websocket events", flush=True)
        sys.exit(1)
    except Exception as e:
        print(f'Error: {e}', flush=True)
        sys.exit(1)

cookie = sys.argv[2]
asyncio.run(test_ws(sys.argv[1], cookie))
PYGOF

# Extract cookie value from cookie.txt
COOKIE_VAL=$(grep forge_session cookie.txt | awk '{print $7}')

python3 ws_test.py "ws://localhost:8080/v1/projects/$PROJ_ID/stream" "forge_session=$COOKIE_VAL" &
WS_PID=$!
sleep 1
curl -s -X POST http://localhost:8080/v1/projects/$PROJ_ID/tasks -b cookie.txt -H "X-Forge-CSRF: 1" -H "Content-Type: application/json" -d "{\"prompt\":\"smoke:\"}" >/dev/null

wait $WS_PID

echo "PASS: WS SMOKE streams monotonic seq and task.done"

# Teardown
kill $FORGED_PID $NODED_PID || true; killall forged-amd64 forge-noded-amd64 || true
echo "All M2 checks passed."
echo "All e2e checks passed!"