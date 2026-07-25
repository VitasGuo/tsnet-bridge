$token = $env:GH_TOKEN
if (-not $token) {
    Write-Host "ERROR: GH_TOKEN environment variable not set. Please set it first:"
    Write-Host "  `$env:GH_TOKEN = `"your_github_token`""
    exit 1
}
$repo = "VitasGuo/tsnet-bridge"
$tag = "v0.2.0"
$headers = @{ Authorization = "Bearer $token"; Accept = "application/vnd.github+json"; "X-GitHub-Api-Version" = "2022-11-28" }

$tagBody = @{ ref = "refs/tags/$tag"; sha = "d7abe7a" } | ConvertTo-Json
Write-Host "=== creating tag ref ==="
try {
    $r = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/git/refs" -Method POST -Headers $headers -Body $tagBody -ContentType "application/json" -TimeoutSec 30
    Write-Host "Tag: $($r.ref)"
} catch { if ($_.Exception.Response.StatusCode -eq 422) { Write-Host "Tag exists" } else { Write-Host "ERR: $($_.Exception.Message)" }
}

$desc = @'
## tsnet-bridge v0.2.0

### What's new
- HTTPS support: new `scheme: https` field for targets
- Tailscale DNS: address supports MagicDNS names
- ICO fix: tray icon renders correctly on Windows 11
- Usage guide and config example updated with DNS/scheme examples

### Full changelog
- bridge.go: Target.Scheme field + targetScheme() helper
- bridge.go: buildHandler() uses t.targetScheme() for URL construction
- config.example.yaml: show DNS name + scheme: https example
- tray.go: usage guide updated with DNS/scheme info
- README.md: new "Connecting via HTTPS or Tailscale DNS" section
- icon.go: fix AND mask row alignment (32->64 bytes) for ICO rendering
'@
$body = @{ tag_name = $tag; target_commitish = "main"; name = $tag; body = $desc; draft = $false; prerelease = $false } | ConvertTo-Json -Depth 5

Write-Host "=== creating release ==="
$release = $null
for ($i = 1; $i -le 3; $i++) {
    try {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases" -Method POST -Headers $headers -Body $body -ContentType "application/json" -TimeoutSec 30
        Write-Host "Release created! ID: $($release.id)"; break
    } catch {
        Write-Host "Attempt $i failed: $($_.Exception.Message)"
        if ($_.Exception.Response.StatusCode -eq 422) {
            try { $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/tags/$tag" -Headers $headers -TimeoutSec 30; Write-Host "Found existing release ID: $($release.id)"; break } catch { }
        }
        Start-Sleep -Seconds 3
    }
}

if ($release) {
    Write-Host "=== uploading exe ==="
    $uploadUrl = $release.upload_url -replace '\{\?name,label\}' , ''
    $fileBytes = [System.IO.File]::ReadAllBytes("$pwd\tsnet-bridge.exe")
    $uploadUri = "${uploadUrl}?name=tsnet-bridge.exe"
    for ($i = 1; $i -le 3; $i++) {
        try {
            $result = Invoke-RestMethod -Uri $uploadUri -Method POST -Headers $headers -Body $fileBytes -ContentType "application/octet-stream" -TimeoutSec 120
            Write-Host "Uploaded! Download: $($result.browser_download_url)"; break
        } catch { Write-Host "Attempt $i failed: $($_.Exception.Message)"; Start-Sleep -Seconds 5 }
    }
    Write-Host "Release URL: $($release.html_url)"
}