$ErrorActionPreference = "Stop"

Write-Host "=== MONEY ==="
go test ./internal/domain -run "Test(NewMoney|ZeroMoney|Money)" -v

if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}
