$ErrorActionPreference = "Stop"

Write-Host "=== EVENTS ==="
go test ./internal/domain -run "Test(NewEvent|MoneySnapshot|WalletBalanceChangedData|Event|PendingReferenceEventData)" -v

if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}
