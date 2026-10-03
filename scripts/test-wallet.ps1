$ErrorActionPreference = "Stop"

Write-Host "=== WALLET ==="
go test ./internal/domain -run "Test(NewWallet|Wallet|RehydrateWallet)" -v

if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}
