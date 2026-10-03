$ErrorActionPreference = "Stop"

Write-Host ""
Write-Host "========================================"
Write-Host " Jungle Backend Challenge - Validation"
Write-Host "========================================"
Write-Host ""

Write-Host "[1/3] gofmt"
gofmt -w .\internal\domain
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host ""
Write-Host "[2/3] go test"
go test ./internal/domain/... -v
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host ""
Write-Host "[3/3] go vet"
go vet ./internal/domain/...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host ""
Write-Host "========================================"
Write-Host " TODAS AS VALIDACOES PASSARAM"
Write-Host "========================================"
