$ErrorActionPreference = "Stop"

Write-Host "=== LEDGER ==="
go test ./internal/domain -run "Test(NewDebitLedger|NewCreditLedger|Ledger)" -v

if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}
