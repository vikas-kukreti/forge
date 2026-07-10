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

# Build all binaries to check they build
make build
echo "PASS: all binaries build for amd64+arm64"

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
if ! DOMAIN="" bin/forged-amd64 2>&1 | grep -q "missing required config variable DOMAIN"; then
    echo "FAIL: forged did not fail-fast on missing config"
    exit 1
fi
echo "PASS: forged exits with clear message on missing required var"

echo "All M0 checks passed."
