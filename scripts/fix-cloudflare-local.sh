#!/bin/bash
# Fix Cloudflare Blocking - Local Setup
# This script starts Byparr and gets fresh Cloudflare cookies for your recorder

set -e

echo "╔════════════════════════════════════════════════════════════╗"
echo "║        🔧 FIX CLOUDFLARE BLOCKING (LOCAL)                  ║"
echo "╚════════════════════════════════════════════════════════════╝"
echo ""

# Check if we're in the right directory
if [ ! -f "docker-compose.yml" ]; then
    echo "❌ docker-compose.yml not found"
    echo "   Please run this script from the project root directory"
    exit 1
fi

# Step 1: Start the full stack
echo "[1/5] Starting Docker Compose stack..."
docker-compose up -d > /dev/null 2>&1
echo "  ✅ Stack started"
echo ""

# Step 2: Wait for Byparr
echo "[2/5] Waiting for Byparr to be ready..."
for i in {1..30}; do
    if curl -fsS --max-time 5 http://localhost:8191/ > /dev/null 2>&1; then
        echo "  ✅ Byparr is ready"
        break
    fi
    echo "  ⏳ Waiting... ($i/30)"
    sleep 2
done
echo ""

# Step 3: Get Cloudflare cookie
echo "[3/5] Getting Cloudflare cookie (this takes 2-3 minutes)..."
echo "  ⏳ Solving Cloudflare challenge..."

RESPONSE=$(curl -sS --fail --max-time 200 -X POST http://localhost:8191/v1 \
    -H 'Content-Type: application/json' \
    -d '{"cmd":"request.get","url":"https://chaturbate.com","maxTimeout":180000}' || echo '{}')

CF_COOKIE=$(echo "$RESPONSE" | jq -r '.solution.cookies[]? | select(.name=="cf_clearance") | .name + "=" + .value' 2>/dev/null || echo "")

if [ -n "$CF_COOKIE" ]; then
    echo "  ✅ Got cf_clearance cookie"
else
    echo "  ⚠️  No cf_clearance cookie found"
    echo "  This might indicate Cloudflare blocking"
fi
echo ""

# Step 4: Wait for recorder
echo "[4/5] Waiting for recorder to be ready..."
for i in {1..30}; do
    if curl -fsS --max-time 5 http://localhost:8080 > /dev/null 2>&1; then
        echo "  ✅ Recorder is ready"
        break
    fi
    echo "  ⏳ Waiting... ($i/30)"
    sleep 2
done
echo ""

# Step 5: Push cookie to recorder
if [ -n "$CF_COOKIE" ]; then
    echo "[5/5] Pushing cookie to recorder..."
    curl -sS --max-time 10 -X POST http://localhost:8080/update_config \
        -H 'Content-Type: application/json' \
        -d "{\"cookies\":\"$CF_COOKIE\"}" > /dev/null 2>&1 && \
        echo "  ✅ Cookie pushed successfully" || \
        echo "  ⚠️  Failed to push cookie"
else
    echo "[5/5] Skipping cookie push (no cookie obtained)"
fi
echo ""

# Success summary
echo "╔════════════════════════════════════════════════════════════╗"
echo "║                    ✅ SETUP COMPLETE                        ║"
echo "╚════════════════════════════════════════════════════════════╝"
echo ""
echo "Your recorder should now be able to bypass Cloudflare!"
echo ""
echo "Next steps:"
echo "  • Open web UI: http://localhost:8080"
echo "  • Add channels to record"
echo "  • Cookie refresher will auto-update every 30 minutes"
echo ""
echo "Troubleshooting:"
echo "  • View recorder logs: docker logs -f chaturbate-dvr"
echo "  • View Byparr logs: docker logs -f byparr-lb"
echo "  • View cookie refresher: docker logs -f cookie-refresher"
echo "  • Restart everything: docker-compose restart"
echo ""
