export FORGE_RUNTIME="local"
export FORGE_PROJECT_ID="testproj"
export FORGE_DATABASE_URL="postgres://forge:password@localhost:5432/forge?sslmode=disable"
export FORGE_NATS_URL="nats://localhost:4222"

mkdir -p /tmp/forge_ws/testproj/ctl
mkdir -p /tmp/forge_ws/testproj/work
bin/forge-shim-amd64 &
SHIM_PID=$!
sleep 2
ls -la /tmp/forge_ws/testproj/ctl
kill $SHIM_PID
