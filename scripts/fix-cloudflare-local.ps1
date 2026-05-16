# Fix Cloudflare Blocking - Local Setup
# This script starts Byparr and gets fresh Cloudflare cookies for your recorder

$ErrorActionPreference = "Stop"

Write-Host "╔════════════════════════════════════════════════════════════╗" -ForegroundColor Cyan
Write-Host "║        🔧 FIX CLOUDFLARE BLOCKING (LOCAL)                  ║" -ForegroundColor Cyan
Write-Host "╚════════════════════════════════════════════════════════════╝" -ForegroundColor Cyan
Write-Host ""

# Check if we're in the right directory
if (-not (Test-Path "docker-compose.yml")) {
    Write-Host "❌ docker-compose.yml not found" -ForegroundColor Red
    Write-Host "   Please run this script from the project root directory" -ForegroundColor Yellow
    exit 1
}

# Step 1: Start the full stack
Write-Host "[1/5] Starting Docker Compose stack..." -ForegroundColor Yellow
try {
    docker-compose up -d 2>&1 | Out-Null
    Write-Host "  ✅ Stack started" -ForegroundColor Green
} catch {
    Write-Host "  ❌ Failed to start stack: $_" -ForegroundColor Red
    exit 1
}
Write-Host ""

# Step 2: Wait for Byparr
Write-Host "[2/5] Waiting for Byparr to be ready..." -ForegroundColor Yellow
$byparrReady = $false
for ($i = 1; $i -le 30; $i++) {
    try {
        $response = Invoke-RestMethod -Uri "http://localhost:8191/" -Method Get -TimeoutSec 5 -ErrorAction Stop
        Write-Host "  ✅ Byparr is ready" -ForegroundColor Green
        $byparrReady = $true
        break
    } catch {
        Write-Host "  ⏳ Waiting... ($i/30)" -ForegroundColor Gray
        Start-Sleep -Seconds 2
    }
}

if (-not $byparrReady) {
    Write-Host "  ❌ Byparr did not start in time" -ForegroundColor Red
    Write-Host "  Check logs: docker logs byparr-lb" -ForegroundColor Yellow
    exit 1
}
Write-Host ""

# Step 3: Get Cloudflare cookie
Write-Host "[3/5] Getting Cloudflare cookie (this takes 2-3 minutes)..." -ForegroundColor Yellow
Write-Host "  ⏳ Solving Cloudflare challenge..." -ForegroundColor Gray

$requestBody = @{
    cmd = "request.get"
    url = "https://chaturbate.com"
    maxTimeout = 180000
} | ConvertTo-Json

try {
    $startTime = Get-Date
    $response = Invoke-RestMethod -Uri "http://localhost:8191/v1" -Method Post -Body $requestBody -ContentType "application/json" -TimeoutSec 200 -ErrorAction Stop
    $duration = ((Get-Date) - $startTime).TotalSeconds
    
    if ($response.status -eq "ok") {
        Write-Host "  ✅ Challenge solved in $([math]::Round($duration, 1))s" -ForegroundColor Green
        
        # Extract cf_clearance cookie
        $cfCookie = $response.solution.cookies | Where-Object { $_.name -eq "cf_clearance" }
        
        if ($cfCookie) {
            $cookieString = "cf_clearance=$($cfCookie.value)"
            Write-Host "  ✅ Got cf_clearance cookie" -ForegroundColor Green
        } else {
            Write-Host "  ⚠️  No cf_clearance cookie found" -ForegroundColor Yellow
            Write-Host "  Trying to proceed anyway..." -ForegroundColor Gray
            $cookieString = ""
        }
    } else {
        Write-Host "  ❌ Failed to solve challenge: $($response.message)" -ForegroundColor Red
        Write-Host ""
        Write-Host "  💡 This usually means:" -ForegroundColor Yellow
        Write-Host "     • Your IP is being blocked by Cloudflare" -ForegroundColor Gray
        Write-Host "     • You need a residential proxy" -ForegroundColor Gray
        Write-Host "     • Or extract cookies manually from your browser" -ForegroundColor Gray
        exit 1
    }
} catch {
    Write-Host "  ❌ Request failed: $_" -ForegroundColor Red
    Write-Host ""
    Write-Host "  💡 Troubleshooting:" -ForegroundColor Yellow
    Write-Host "     1. Check Byparr logs: docker logs byparr-lb" -ForegroundColor Gray
    Write-Host "     2. Try restarting: docker-compose restart byparr byparr-lb" -ForegroundColor Gray
    Write-Host "     3. Or extract cookies manually (see below)" -ForegroundColor Gray
    exit 1
}
Write-Host ""

# Step 4: Wait for recorder
Write-Host "[4/5] Waiting for recorder to be ready..." -ForegroundColor Yellow
$recorderReady = $false
for ($i = 1; $i -le 30; $i++) {
    try {
        $response = Invoke-RestMethod -Uri "http://localhost:8080" -Method Get -TimeoutSec 5 -ErrorAction Stop
        Write-Host "  ✅ Recorder is ready" -ForegroundColor Green
        $recorderReady = $true
        break
    } catch {
        Write-Host "  ⏳ Waiting... ($i/30)" -ForegroundColor Gray
        Start-Sleep -Seconds 2
    }
}

if (-not $recorderReady) {
    Write-Host "  ❌ Recorder did not start in time" -ForegroundColor Red
    Write-Host "  Check logs: docker logs chaturbate-dvr" -ForegroundColor Yellow
    exit 1
}
Write-Host ""

# Step 5: Push cookie to recorder
if ($cookieString) {
    Write-Host "[5/5] Pushing cookie to recorder..." -ForegroundColor Yellow
    
    $configBody = @{
        cookies = $cookieString
    } | ConvertTo-Json
    
    try {
        Invoke-RestMethod -Uri "http://localhost:8080/update_config" -Method Post -Body $configBody -ContentType "application/json" -TimeoutSec 10 -ErrorAction Stop | Out-Null
        Write-Host "  ✅ Cookie pushed successfully" -ForegroundColor Green
    } catch {
        Write-Host "  ⚠️  Failed to push cookie: $_" -ForegroundColor Yellow
        Write-Host "  The recorder might still work if cookie-refresher is running" -ForegroundColor Gray
    }
} else {
    Write-Host "[5/5] Skipping cookie push (no cookie obtained)" -ForegroundColor Yellow
}
Write-Host ""

# Success summary
Write-Host "╔════════════════════════════════════════════════════════════╗" -ForegroundColor Green
Write-Host "║                    ✅ SETUP COMPLETE                        ║" -ForegroundColor Green
Write-Host "╚════════════════════════════════════════════════════════════╝" -ForegroundColor Green
Write-Host ""
Write-Host "Your recorder should now be able to bypass Cloudflare!" -ForegroundColor White
Write-Host ""
Write-Host "Next steps:" -ForegroundColor White
Write-Host "  • Open web UI: http://localhost:8080" -ForegroundColor Gray
Write-Host "  • Add channels to record" -ForegroundColor Gray
Write-Host "  • Cookie refresher will auto-update every 30 minutes" -ForegroundColor Gray
Write-Host ""
Write-Host "Troubleshooting:" -ForegroundColor White
Write-Host "  • View recorder logs: docker logs -f chaturbate-dvr" -ForegroundColor Gray
Write-Host "  • View Byparr logs: docker logs -f byparr-lb" -ForegroundColor Gray
Write-Host "  • View cookie refresher: docker logs -f cookie-refresher" -ForegroundColor Gray
Write-Host "  • Restart everything: docker-compose restart" -ForegroundColor Gray
Write-Host ""

# Alternative: Manual cookie extraction
Write-Host "═══════════════════════════════════════════════════════════" -ForegroundColor Cyan
Write-Host "ALTERNATIVE: Manual Cookie Extraction" -ForegroundColor Cyan
Write-Host "═══════════════════════════════════════════════════════════" -ForegroundColor Cyan
Write-Host ""
Write-Host "If Byparr keeps failing, extract cookies manually:" -ForegroundColor White
Write-Host ""
Write-Host "1. Open Chrome/Firefox" -ForegroundColor Gray
Write-Host "2. Visit https://chaturbate.com" -ForegroundColor Gray
Write-Host "3. Complete Cloudflare challenge" -ForegroundColor Gray
Write-Host "4. Press F12 → Application → Cookies" -ForegroundColor Gray
Write-Host "5. Copy 'cf_clearance' cookie value" -ForegroundColor Gray
Write-Host "6. Run this command:" -ForegroundColor Gray
Write-Host ""
Write-Host '   $cookie = "cf_clearance=YOUR_VALUE_HERE"' -ForegroundColor Yellow
Write-Host '   $body = @{cookies=$cookie} | ConvertTo-Json' -ForegroundColor Yellow
Write-Host '   Invoke-RestMethod -Uri "http://localhost:8080/update_config" -Method Post -Body $body -ContentType "application/json"' -ForegroundColor Yellow
Write-Host ""
Write-Host "Note: Manual cookies expire after ~30 minutes" -ForegroundColor Gray
Write-Host ""
