# Generate thumbnails for videos that don't have them
# Usage: .\scripts\generate-missing-thumbnails.ps1

$ErrorActionPreference = "Stop"

Write-Host "╔════════════════════════════════════════════════════════════╗" -ForegroundColor Cyan
Write-Host "║        📸 GENERATE MISSING THUMBNAILS                     ║" -ForegroundColor Cyan
Write-Host "╚════════════════════════════════════════════════════════════╝" -ForegroundColor Cyan
Write-Host ""

# Load environment variables
if (Test-Path ".env") {
    Get-Content ".env" | ForEach-Object {
        if ($_ -match '^\s*([^#][^=]+)=(.*)$') {
            $name = $matches[1].Trim()
            $value = $matches[2].Trim()
            [Environment]::SetEnvironmentVariable($name, $value, "Process")
        }
    }
}

$SUPABASE_URL = $env:SUPABASE_URL
$SUPABASE_API_KEY = $env:SUPABASE_API_KEY

if (-not $SUPABASE_URL -or -not $SUPABASE_API_KEY) {
    Write-Host "❌ Supabase credentials not found" -ForegroundColor Red
    Write-Host ""
    Write-Host "Set them in .env file or as environment variables" -ForegroundColor Yellow
    exit 1
}

Write-Host "📡 Fetching videos from Supabase..." -ForegroundColor Yellow

try {
    $response = Invoke-RestMethod -Uri "$SUPABASE_URL/rest/v1/gofile_uploads?select=*" `
        -Method Get `
        -Headers @{
            "apikey" = $SUPABASE_API_KEY
            "Content-Type" = "application/json"
        } -ErrorAction Stop
    
    Write-Host "✅ Found $($response.Count) videos" -ForegroundColor Green
    Write-Host ""
    
    $missingThumbnails = $response | Where-Object { -not $_.thumbnail_link -or $_.thumbnail_link -eq "" }
    
    if ($missingThumbnails.Count -eq 0) {
        Write-Host "✅ All videos have thumbnails!" -ForegroundColor Green
        exit 0
    }
    
    Write-Host "⚠️  Found $($missingThumbnails.Count) videos without thumbnails:" -ForegroundColor Yellow
    Write-Host ""
    
    foreach ($video in $missingThumbnails) {
        Write-Host "  • $($video.streamer_name) - $($video.filename)" -ForegroundColor Gray
    }
    
    Write-Host ""
    Write-Host "═══════════════════════════════════════════════════════════" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "To generate thumbnails, you need to:" -ForegroundColor White
    Write-Host ""
    Write-Host "1. Make sure the video files still exist locally" -ForegroundColor Gray
    Write-Host "2. Trigger the upload process again with:" -ForegroundColor Gray
    Write-Host "   curl -X POST http://localhost:8080/api/upload/completed" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "Or manually generate thumbnails using FFmpeg:" -ForegroundColor Gray
    Write-Host "   ffmpeg -i video.mp4 -ss 00:00:10 -vframes 1 -q:v 2 thumbnail.jpg" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "Then upload to Pixhost.to and update Supabase" -ForegroundColor Gray
    Write-Host ""
    
} catch {
    Write-Host "❌ Error: $_" -ForegroundColor Red
    exit 1
}
