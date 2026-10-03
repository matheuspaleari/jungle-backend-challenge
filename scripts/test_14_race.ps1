$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$report = Join-Path $root "race_14_validation.txt"
"#14 ROUND 2 - RACE DETECTOR (LINUX/DOCKER)" | Set-Content $report
"Started: $(Get-Date -Format o)" | Add-Content $report

function Fail([string]$Message) {
    "[FAIL] $Message" | Tee-Object -FilePath $report -Append
    throw $Message
}
function Pass([string]$Message) {
    "[PASS] $Message" | Tee-Object -FilePath $report -Append
}

docker version *> $null
if ($LASTEXITCODE -ne 0) { Fail "Docker nao esta disponivel" }
Pass "Docker disponivel"

$project = (Resolve-Path $root).Path
$image = "golang:1.27.1-bookworm"

Write-Host ""
Write-Host "=== GO VERSION IN CONTAINER ==="
$old = $ErrorActionPreference
$ErrorActionPreference = "Continue"
$out = & docker run --rm $image go version 2>&1
$code = $LASTEXITCODE
$ErrorActionPreference = $old
$out | ForEach-Object { "$_" } | Tee-Object -FilePath $report -Append | Write-Host
if ($code -ne 0) { Fail "go version no container" }
Pass "toolchain Linux validado"

Write-Host ""
Write-Host "=== GO TEST -RACE IN LINUX ==="

# Do not invoke a shell here. The official Go image configures the Go binary
# correctly for direct execution; using `sh -lc` can reset PATH on some image
# variants. `go test` itself accepts the package and test flags directly.
$old = $ErrorActionPreference
$ErrorActionPreference = "Continue"
$out = & docker run --rm `
    -v "${project}:/workspace" `
    -w /workspace `
    -e CGO_ENABLED=1 `
    $image `
    go test -race ./... -count=1 2>&1
$code = $LASTEXITCODE
$ErrorActionPreference = $old

$out | ForEach-Object { "$_" } | Tee-Object -FilePath $report -Append | Write-Host

if ($code -ne 0) {
    Fail "go test -race ./... (exit code $code)"
}

Pass "go test -race ./..."
"`r`n14_RACE_RESULT=PASS" | Add-Content $report

Write-Host ""
Write-Host "#14 race validation PASS"
Write-Host "Report: $report"
