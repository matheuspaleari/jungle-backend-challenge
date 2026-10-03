$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$report = Join-Path $root "final_14_validation.txt"
"#14 FINAL VALIDATION" | Set-Content $report
"Started: $(Get-Date -Format o)" | Add-Content $report

function Pass([string]$Message) {
    "[PASS] $Message" | Tee-Object -FilePath $report -Append
}

function Fail([string]$Message) {
    "[FAIL] $Message" | Tee-Object -FilePath $report -Append
    throw $Message
}

Write-Host ""
Write-Host "=== APPLY MIGRATIONS ==="

# Windows PowerShell can promote native stderr (including harmless PostgreSQL
# NOTICE messages) to NativeCommandError while ErrorActionPreference=Stop.
# Run the migration script in a child PowerShell with stderr merged there,
# capture its real exit code, then restore strict error handling.
$previousErrorActionPreference = $ErrorActionPreference
$ErrorActionPreference = "Continue"

$migrationOutput = & powershell.exe `
    -NoProfile `
    -ExecutionPolicy Bypass `
    -Command "& '.\scripts\migrate-up.ps1' 2>&1; exit `$LASTEXITCODE"

$migrationExitCode = $LASTEXITCODE
$ErrorActionPreference = $previousErrorActionPreference

$migrationOutput |
    ForEach-Object { "$_" } |
    Tee-Object -FilePath $report -Append |
    Write-Host

if ($migrationExitCode -ne 0) {
    Fail "migrations (exit code $migrationExitCode)"
}
Pass "migrations"

# Do not assume whether 000011 was already applied by the previous interrupted
# validation. Migrations are idempotently controlled by schema_migrations.
$version = docker exec jungle-postgres psql -U jungle -d jungle -t -A `
    -c "SELECT EXISTS (
        SELECT 1
        FROM schema_migrations
        WHERE version IN ('11', '000011')
    );"

if ($LASTEXITCODE -ne 0) {
    Fail "nao foi possivel consultar schema_migrations"
}
if ($version.Trim() -ne "t") {
    Fail "migration 000011 nao registrada"
}
Pass "migration 000011 registrada"

$legacyConstraint = docker exec jungle-postgres psql -U jungle -d jungle -t -A `
    -c "SELECT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'wallets_player_currency_unique'
    );"

if ($LASTEXITCODE -ne 0) {
    Fail "nao foi possivel consultar constraint global da wallet"
}
if ($legacyConstraint.Trim() -ne "f") {
    Fail "constraint global wallets_player_currency_unique ainda existe"
}
Pass "constraint global de wallet removida"

$providerIndex = docker exec jungle-postgres psql -U jungle -d jungle -t -A `
    -c "SELECT EXISTS (
        SELECT 1
        FROM pg_indexes
        WHERE indexname = 'wallets_provider_player_currency_unique'
    );"

if ($LASTEXITCODE -ne 0) {
    Fail "nao foi possivel consultar indice provider-scoped"
}
if ($providerIndex.Trim() -ne "t") {
    Fail "indice provider-scoped de wallet ausente"
}
Pass "unicidade provider-scoped presente"

$env:TEST_DATABASE_URL = if ($env:TEST_DATABASE_URL) {
    $env:TEST_DATABASE_URL
} else {
    "postgres://jungle:jungle@localhost:5432/jungle?sslmode=disable"
}

Write-Host ""
Write-Host "=== PROVIDER WALLET ISOLATION TEST ==="

& go test ./internal/infrastructure/postgres `
    -run TestWalletRepositoryAllowsSamePlayerAndCurrencyAcrossProvidersIntegration `
    -count=1 -v 2>&1 |
    Tee-Object -FilePath $report -Append

if ($LASTEXITCODE -ne 0) {
    Fail "provider wallet isolation test"
}
Pass "dois providers podem usar mesmo player/currency"

Write-Host ""
Write-Host "=== PENDING REFERENCE TESTS ==="

& go test ./internal/application `
    -run "TestPendingReferenceWorker" `
    -count=1 -v 2>&1 |
    Tee-Object -FilePath $report -Append

if ($LASTEXITCODE -ne 0) {
    Fail "pending reference tests"
}
Pass "pending reference worker integration tests"

Write-Host ""
Write-Host "=== ALL TESTS ==="

& go test ./... -count=1 2>&1 |
    Tee-Object -FilePath $report -Append

if ($LASTEXITCODE -ne 0) {
    Fail "go test ./..."
}
Pass "go test ./..."

& go vet ./... 2>&1 |
    Tee-Object -FilePath $report -Append

if ($LASTEXITCODE -ne 0) {
    Fail "go vet ./..."
}
Pass "go vet ./..."

& go build ./cmd/api 2>&1 |
    Tee-Object -FilePath $report -Append

if ($LASTEXITCODE -ne 0) {
    Fail "go build ./cmd/api"
}
Pass "go build ./cmd/api"

$module = Get-Content -Raw ".\internal\platform\app\module.go"
if (
    $module -notmatch "newPendingReferenceWorker" -or
    $module -notmatch "runPendingReferenceLoop"
) {
    Fail "PendingReferenceWorker nao esta ligado ao lifecycle"
}
Pass "PendingReferenceWorker ligado ao Fx lifecycle"

$ignored = @(
    "api.exe",
    ".tmp-12",
    "distributed_12_validation.txt",
    "distributed_12_e2e.txt",
    "audit_14_detailed.txt",
    "final_14_validation.txt"
)

foreach ($path in $ignored) {
    & git check-ignore -q -- $path
    if ($LASTEXITCODE -ne 0) {
        Fail ".gitignore nao cobre $path"
    }
}
Pass ".gitignore cobre artefatos de runtime"

"`r`n14_ROUND1_RESULT=PASS" | Add-Content $report

Write-Host ""
Write-Host "#14 round 1 validation PASS"
Write-Host "Report: $report"
