$ErrorActionPreference = "Stop"

Write-Host "=== WAGER TRANSACTION ==="
go test ./internal/domain -run "Test(NewBet|Opening|Loss|BetRejects|Refund|Rollback|PendingReference|BetCan|Transaction|Rejection|WinMay)" -v

if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}
