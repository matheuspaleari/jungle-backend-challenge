$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$report = Join-Path $root "distributed_12_validation.txt"

function Run-Cmd([string]$Label, [string]$Command) {
    Add-Content $report "`r`n== $Label =="
    $output = cmd.exe /d /c "$Command 2>&1"
    $code = $LASTEXITCODE
    $output | Tee-Object -FilePath $report -Append
    if ($code -ne 0) { throw "$Label failed with exit code $code" }
}

"#12 DISTRIBUTED VALIDATION" | Set-Content $report
"Started: $(Get-Date -Format o)" | Add-Content $report

Run-Cmd "GO TEST" "go test ./... -count=1"
Run-Cmd "INBOX ATOMICITY" "go test ./internal/infrastructure/postgres -run TestInboxReceiveAndCompleteRollbackTogetherIntegration -count=1 -v"
Run-Cmd "OUTBOX CLAIM/LEASE RECOVERY" "go test ./internal/infrastructure/postgres -run TestOutboxClaimIsExclusiveAcrossPublishersAndExpiredLeaseRecovers -count=1 -v"
Run-Cmd "OUTBOX PUBLISH/CONFIRM RECOVERY" "go test ./internal/infrastructure/outboxpublisher -run TestPublisher -count=1 -v"
Run-Cmd "THREE INDEPENDENT DB CLIENTS" "go test ./internal/application -run TestWagerProcessorThreeIndependentClientsPreserveWalletInvariantIntegration -count=1 -v"
Run-Cmd "GO VET" "go vet ./..."
Run-Cmd "GO BUILD" "go build ./cmd/api"

"`r`n12_VALIDATION_RESULT=PASS" | Add-Content $report
Write-Host "#12 validation PASS"
Write-Host "Report: $report"
