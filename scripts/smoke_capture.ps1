param(
    [string]$BaseUrl = 'http://127.0.0.1:8765',
    [string]$RepositoryUrl = 'https://github.com/octocat/Hello-World',
    [string]$Output = 'work/capture-smoke.json'
)

$ErrorActionPreference = 'Stop'
$session = Invoke-RestMethod -Uri "$BaseUrl/v1/session" -SessionVariable webSession
$headers = @{ 'X-CSRF-Token' = $session.csrf_token }
$body = @{
    kind = 'github'
    url = $RepositoryUrl
    privacy_mode = 'strict_local'
} | ConvertTo-Json -Compress

$accepted = Invoke-RestMethod -Uri "$BaseUrl/v1/repositories" -Method Post -Body $body -ContentType 'application/json' -Headers $headers -WebSession $webSession
$job = $null
for ($attempt = 0; $attempt -lt 120; $attempt++) {
    Start-Sleep -Seconds 1
    $job = Invoke-RestMethod -Uri "$BaseUrl/v1/jobs/$($accepted.job_id)" -WebSession $webSession
    if ($job.state -in @('completed', 'cancelled', 'discarded')) { break }
}
if ($job.state -ne 'completed') { throw "Capture job did not complete: $($job.state)" }

$prefix = "$BaseUrl/v1/repositories/$($accepted.repository_id)/snapshots/$($accepted.snapshot_id)"
$snapshot = Invoke-RestMethod -Uri $prefix -WebSession $webSession
$coverage = Invoke-RestMethod -Uri "$prefix/coverage" -WebSession $webSession
if ($snapshot.state -ne 'ready' -or -not $snapshot.manifest_hash) { throw 'Snapshot is not ready' }
if ($snapshot.source_kind -ne 'commit') { throw 'GitHub capture did not resolve a commit' }
if ($coverage.inventory_count -le 0 -or $coverage.status_counts.analyzed -le 0) { throw 'No searchable source inventory' }
$search = Invoke-RestMethod -Uri "$prefix/search?q=Hello" -WebSession $webSession
if ($search.results.Count -le 0) { throw 'No source search result for the public fixture' }
$evidence = Invoke-RestMethod -Uri "$prefix/evidence/$($search.results[0].evidence_id)" -WebSession $webSession
if (-not $evidence.sanitized -or $evidence.source_ranges[0].artifact_id -ne 'README') { throw 'Search result did not resolve to source evidence' }

$result = [pscustomobject]@{
    source = $RepositoryUrl
    repository_id = $accepted.repository_id
    snapshot_id = $accepted.snapshot_id
    job_id = $accepted.job_id
    manifest_hash = $snapshot.manifest_hash
    source_kind = $snapshot.source_kind
    inventory_count = $coverage.inventory_count
    analyzed = $coverage.status_counts.analyzed
    pending = $coverage.status_counts.pending
    excluded = $coverage.status_counts.excluded
}
$path = [System.IO.Path]::GetFullPath($Output)
[System.IO.Directory]::CreateDirectory([System.IO.Path]::GetDirectoryName($path)) | Out-Null
$result | ConvertTo-Json -Depth 4 | Set-Content -Path $path -Encoding utf8
$result | Format-Table source, job_id, source_kind, inventory_count, analyzed, excluded
