#Requires -Version 5.1

$ErrorActionPreference = "Stop"

$envFile = Join-Path $PSScriptRoot ".env"
$vars = @{}
Get-Content $envFile | ForEach-Object {
    if ($_ -match '^\s*([^#=]+)=\s*"(.+)"\s*$') {
        $vars[$matches[1]] = $matches[2]
    }
}

$baseUrl = $vars["SUPABASE_URL"].TrimEnd('/')
$apiKey = $vars["SUPABASE_SERVICE_ROLE_KEY"]
$headers = @{
    "apikey"        = $apiKey
    "Authorization" = "Bearer $apiKey"
    "Content-Type"  = "application/json"
    "Prefer"        = "return=minimal"
}

function Invoke-Supabase($method, $path) {
    $url = "$baseUrl/rest/v1$path"
    try {
        $resp = Invoke-RestMethod -Uri $url -Method $method -Headers $headers -SkipHttpErrorCheck
        return $resp
    } catch {
        return $null
    }
}

Write-Host "=== Fetching all recordings ==="
$all = Invoke-Supabase GET "/recordings?order=timestamp.desc"
if (-not $all) {
    Write-Host "No recordings found or query failed."
    exit 1
}
Write-Host "Total recordings: $($all.Count)"

$corrupted = @()
foreach ($rec in $all) {
    $reasons = @()
    if ([string]::IsNullOrWhiteSpace($rec.embed_url)) { $reasons += "no embed_url" }
    if (!$rec.filesize -or $rec.filesize -eq 0) { $reasons += "filesize=0" }
    if (!$rec.duration -or $rec.duration -eq 0) { $reasons += "duration=0" }
    if ($rec.duration -and $rec.duration -lt 10) { $reasons += "duration<10s ($($rec.duration)s)" }
    if ($reasons.Count -gt 0) {
        $corrupted += [PSCustomObject]@{
            Filename    = $rec.filename
            Username    = $rec.username
            Timestamp   = $rec.timestamp
            Filesize    = if ($rec.filesize) { $rec.filesize } else { 0 }
            Duration    = if ($rec.duration) { $rec.duration } else { 0 }
            EmbedURL    = if ($rec.embed_url) { $rec.embed_url.Substring(0, [Math]::Min(60, $rec.embed_url.Length)) } else { "<empty>" }
            Reasons     = $reasons -join "; "
        }
    }
}

if ($corrupted.Count -eq 0) {
    Write-Host "`nNo corrupted recordings found."
    exit 0
}

Write-Host "`n=== Corrupted recordings: $($corrupted.Count) ==="
$corrupted | Format-Table Filename, Username, Filesize, Duration, Reasons -AutoSize -Wrap

$confirm = Read-Host "`nDelete these $($corrupted.Count) recordings? (y/N)"
if ($confirm -ne 'y') {
    Write-Host "Aborted."
    exit 0
}

$deleted = 0
foreach ($rec in $corrupted) {
    $fname = $rec.Filename
    Write-Host "  deleting $fname... " -NoNewline

    # upload_links
    Invoke-Supabase DELETE "/upload_links?filename=eq.$([System.Web.HttpUtility]::UrlEncode($fname))"
    # preview_images
    Invoke-Supabase DELETE "/preview_images?filename=eq.$([System.Web.HttpUtility]::UrlEncode($fname))"
    # pipeline_states
    Invoke-Supabase DELETE "/pipeline_states?file_hash=eq.$([System.Web.HttpUtility]::UrlEncode($fname))"
    # upload_journal
    Invoke-Supabase DELETE "/upload_journal?filename=eq.$([System.Web.HttpUtility]::UrlEncode($fname))"
    # the recording itself
    Invoke-Supabase DELETE "/recordings?filename=eq.$([System.Web.HttpUtility]::UrlEncode($fname))"

    Write-Host "OK"
    $deleted++
}

Write-Host "`nDeleted $deleted corrupted recordings."
