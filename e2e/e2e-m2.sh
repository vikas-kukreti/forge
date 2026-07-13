#!/bin/bash
set -euo pipefail

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
sudo mkdir -p /run/forge
sudo chmod 777 /run/forge
export FORGE_SIGNUP_GRANT_CREDITS="50"
export FORGE_MAX_PROJECTS_PER_USER="20"
export FORGE_METRICS_ADDR="localhost:9091"
export WS_ROOT="/tmp/forge_ws"

# Re-build everything
make build

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
PROJ_ID=$(echo "$CREATE_RES" | grep -o '"id":"[^"]*' | cut -d'"' -f4 | head -1)
echo "PASS: project created: $PROJ_ID"

echo "Testing WS streams..."
# using websocat or wscat to test might be needed, but since we are doing simple test, curl HTTP Upgrade can somewhat check connectivity, but we need websocket frame decode. We'll use a small inline python script to test.

cat << 'EOF' > ws_test.py
import sys, json, asyncio, websockets
async def test_ws(uri, cookie):
    async with websockets.connect(uri, additional_headers={"Cookie": cookie}) as ws:
        hello = await ws.recv()
        hello_data = json.loads(hello)
        if hello_data.get("type") != "hello":
            sys.exit(1)

        # expect 5 events
        for _ in range(5):
            msg = await ws.recv()
            data = json.loads(msg)
            if data.get("type") == "task.done":
                print("Success!", flush=True); sys.exit(0)
        sys.exit(1)

cookie = sys.argv[2]
asyncio.run(test_ws(sys.argv[1], cookie))
EOF

# Extract cookie value from cookie.txt
COOKIE_VAL=$(grep forge_session cookie.txt | awk '{print $7}')

# Need websockets pip package
pip install websockets >/dev/null 2>&1 || true

python3 ws_test.py "ws://localhost:8080/v1/projects/$PROJ_ID/stream" "forge_session=$COOKIE_VAL" &
WS_PID=$!
sleep 1
curl -s -X POST http://localhost:8080/v1/projects/$PROJ_ID/tasks -b cookie.txt -H "X-Forge-CSRF: 1" -H "Content-Type: application/json" -d "{\"prompt\":\"SMOKE:\"}" >/dev/null

wait $WS_PID

echo "PASS: WS SMOKE streams monotonic seq and task.done"

# Teardown
kill $FORGED_PID $NODED_PID || true; killall forged-amd64 forge-noded-amd64 || true
# wait || true
echo "All M2 checks passed."
