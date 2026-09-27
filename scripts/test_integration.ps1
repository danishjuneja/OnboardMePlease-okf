param([switch]$Live)
$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot -Parent)
New-Item -ItemType Directory -Force work | Out-Null
$previousOS = $env:GOOS
$previousArch = $env:GOARCH
$previousCGO = $env:CGO_ENABLED
try {
    $env:GOOS = 'linux'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    go test -c ./internal/knowledge -o work/knowledge.integration.test
    if ($LASTEXITCODE -ne 0) { throw 'Integration test compilation failed' }
} finally {
    $env:GOOS = $previousOS
    $env:GOARCH = $previousArch
    $env:CGO_ENABLED = $previousCGO
}
docker compose cp work/knowledge.integration.test app:/tmp/knowledge.integration.test
if ($LASTEXITCODE -ne 0) { throw 'Start the Compose app before running integration tests' }
docker compose cp testdata/mixed-monolith app:/tmp/mixed-monolith
docker compose cp testdata/evals app:/tmp/evals
docker compose exec -T -e OMP_TEST_DB=1 -e OMP_FIXTURE_ROOT=/tmp/mixed-monolith app /tmp/knowledge.integration.test '-test.run=TestDatabase' '-test.v'
if ($LASTEXITCODE -ne 0) { throw 'Database integration test failed' }
if ($Live) {
    # Explicit opt-in: this makes billable requests for synthetic source only.
    docker compose exec -T -e OMP_TEST_DB=1 -e OMP_LIVE_EVAL=1 -e OMP_FIXTURE_ROOT=/tmp/mixed-monolith -e OMP_EVAL_ROOT=/tmp/evals -e OMP_EVAL_OUTPUT=/tmp/live-evaluation.json app /tmp/knowledge.integration.test '-test.run=TestLiveArchitectureEvaluation' '-test.v' '-test.timeout=25m'
    $evaluationExit = $LASTEXITCODE
    docker compose cp app:/tmp/live-evaluation.json work/live-evaluation.json
    if ($evaluationExit -ne 0) { throw 'Live evaluation failed; inspect retained report if available' }
    Write-Output 'Review work/live-evaluation.json against testdata/evals/cases.json; test completion alone does not certify factual correctness.'
}
