param(
    [string]$BaseUrl = 'http://127.0.0.1:8765',
    [string]$CaptureResult = 'work/capture-smoke.json'
)

$ErrorActionPreference = 'Stop'
$capture = Get-Content -LiteralPath $CaptureResult -Raw | ConvertFrom-Json
$session = Invoke-RestMethod -Uri "$BaseUrl/v1/session" -SessionVariable webSession
$prefix = "$BaseUrl/v1/repositories/$($capture.repository_id)/snapshots/$($capture.snapshot_id)"
$overview = Invoke-RestMethod -Uri "$prefix/overview" -WebSession $webSession
if ($overview.snapshot_id -ne $capture.snapshot_id -or $overview.inventory.total -ne $capture.inventory_count) {
    throw 'Overview scope or inventory does not match the capture'
}
$statusTotal = ($overview.inventory.statuses.PSObject.Properties | Measure-Object -Property Value -Sum).Sum
$categoryTotal = ($overview.inventory.categories.PSObject.Properties | Measure-Object -Property Value -Sum).Sum
if ($statusTotal -ne $overview.inventory.total -or $categoryTotal -ne $overview.inventory.total) {
    throw 'Overview did not account for every artifact'
}
$seen = 0
do {
    $page = Invoke-RestMethod -Uri "$prefix/overview/inventory?offset=$seen&limit=100" -WebSession $webSession
    $seen += $page.artifacts.Count
} while ($page.artifacts.Count -eq 100)
if ($seen -ne $overview.inventory.total) { throw 'Paged inventory omitted artifacts' }
$other = (Invoke-RestMethod -Uri "$BaseUrl/v1/repositories" -WebSession $webSession).repositories |
    Where-Object { $_.repository_id -ne $capture.repository_id } | Select-Object -First 1
if ($other) {
    foreach ($suffix in @('overview', 'overview/inventory', 'knowledge/export')) {
        try {
            Invoke-WebRequest -Uri "$BaseUrl/v1/repositories/$($other.repository_id)/snapshots/$($capture.snapshot_id)/$suffix" -WebSession $webSession | Out-Null
            throw "Cross-repository $suffix request unexpectedly succeeded"
        } catch {
            if (-not $_.Exception.Response -or [int]$_.Exception.Response.StatusCode -ne 404) { throw }
        }
    }
}
foreach ($claim in $overview.claims) {
    if ($claim.kind -eq 'fact' -and $claim.evidence_ids.Count -eq 0) { throw 'Factual claim has no citation' }
    foreach ($evidenceId in $claim.evidence_ids) {
        $source = Invoke-RestMethod -Uri "$prefix/evidence/$evidenceId" -WebSession $webSession
        if ($source.evidence_id -ne $evidenceId) { throw 'Overview citation did not resolve' }
    }
}
$zipPath = Join-Path (Resolve-Path 'work') 'overview-smoke.zip'
try {
    Invoke-WebRequest -Uri "$prefix/knowledge/export" -WebSession $webSession -OutFile $zipPath | Out-Null
    python scripts/validate_okf.py $zipPath
    if ($LASTEXITCODE -ne 0) { throw 'OKF structural validator failed' }
    $archive = [System.IO.Compression.ZipFile]::OpenRead($zipPath)
    try {
        if (-not ($archive.Entries | Where-Object FullName -EQ 'index.md')) { throw 'OKF root index missing' }
        if (-not ($archive.Entries | Where-Object FullName -EQ 'concepts/coverage.md')) { throw 'OKF coverage concept missing' }
        foreach ($entry in $archive.Entries | Where-Object { $_.FullName -like 'concepts/*' }) {
            $reader = [System.IO.StreamReader]::new($entry.Open())
            try { $content = $reader.ReadToEnd() } finally { $reader.Dispose() }
            if (-not $content.StartsWith("---`ntype: ")) { throw "Invalid OKF frontmatter in $($entry.FullName)" }
            if ($content -match '(?m)^verified:') { throw 'Export invented a verification event' }
        }
    } finally { $archive.Dispose() }
} finally {
    if (Test-Path -LiteralPath $zipPath) { Remove-Item -LiteralPath $zipPath }
}

[pscustomobject]@{ snapshot_id = $overview.snapshot_id; version = $overview.version; artifacts = $seen; claims = $overview.claims.Count; sections = $overview.sections.Count } |
    Format-Table snapshot_id, version, artifacts, claims, sections
