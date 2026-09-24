param(
    [string]$BaseUrl = 'http://127.0.0.1:8765',
    [string]$ResultsFile = 'work/phase1-smoke.json'
)

$ErrorActionPreference = 'Stop'
$healthy = $false
for ($attempt = 0; $attempt -lt 30; $attempt++) {
    try {
        $health = Invoke-RestMethod -Uri "$BaseUrl/healthz" -TimeoutSec 2
        if ($health.status -eq 'ok') { $healthy = $true; break }
    } catch {}
    Start-Sleep -Seconds 1
}
if (-not $healthy) { throw 'Application did not recover after database restart' }

$null = Invoke-RestMethod -Uri "$BaseUrl/v1/session" -SessionVariable webSession
$results = Get-Content -Path $ResultsFile -Raw | ConvertFrom-Json
foreach ($item in $results) {
    $job = Invoke-RestMethod -Uri "$BaseUrl/v1/jobs/$($item.job_id)" -WebSession $webSession
    $prefix = "$BaseUrl/v1/repositories/$($item.repository_id)/snapshots/$($item.snapshot_id)"
    $snapshot = Invoke-RestMethod -Uri $prefix -WebSession $webSession
    $coverage = Invoke-RestMethod -Uri "$prefix/coverage" -WebSession $webSession
    if ($job.state -ne 'completed' -or $snapshot.state -ne 'ready' -or
        $snapshot.manifest_hash -ne $item.manifest_hash -or
        $coverage.inventory_count -ne $item.inventory_count -or
        $coverage.status_counts.analyzed -ne $item.analyzed) {
        throw "Persistence mismatch for $($item.source)"
    }
    Write-Output "$($item.source): job completed, snapshot ready, $($coverage.inventory_count) artifacts persisted"
}
