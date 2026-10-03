$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$report = Join-Path $root "requirements_14_round2.txt"
"#14 ROUND 2 - REQUIREMENT EVIDENCE AUDIT" | Set-Content $report
"Generated: $(Get-Date -Format o)" | Add-Content $report

function Section([string]$Name) {
    "`r`n=== $Name ===" | Add-Content $report
}

function Search-Go([string]$Pattern) {
    Get-ChildItem ".\internal" -Recurse -Filter "*.go" |
        Select-String -Pattern $Pattern |
        ForEach-Object {
            "$($_.Path):$($_.LineNumber): $($_.Line.Trim())"
        } |
        Add-Content $report
}

Section "CROSS TRANSPORT / CANONICAL HASH"
Search-Go "Canonical|BusinessHash|PayloadHash|Idempotency|WagerPayload"

Section "OPENING ZERO / POSITIVE"
Search-Go "OPENING|Opening|initialBalance|InitialBalance"

Section "REFUND / ROLLBACK / REVERSAL"
Search-Go "REFUND|ROLLBACK|Refund|Rollback|reversal|Reversal"

Section "PENDING REFERENCE"
Search-Go "PENDING_REFERENCE|PendingReference"

Section "OUTBOX RECOVERY"
Search-Go "MarkPublished|MarkFailed|ClaimPending|locked_until|eventId|EventID"

Section "INBOX ATOMICITY"
Search-Go "Inbox|ReceiveAndCompleteRollbackTogether|CompleteTx|ReceiveTx"

Section "CONCURRENCY"
Search-Go "ThreeIndependent|IndependentWallet|Concurrent|concurr"

Section "AUTH FAIL CLOSED / PROVIDER IDENTITY"
Search-Go "authenticatedProviderID|requireWalletOwner|AuthenticationMiddleware|ProviderID"

Section "TEST FUNCTIONS"
Get-ChildItem ".\internal" -Recurse -Filter "*_test.go" |
    Select-String -Pattern "^func Test" |
    ForEach-Object {
        "$($_.Path):$($_.LineNumber): $($_.Line.Trim())"
    } |
    Add-Content $report

Section "MIGRATIONS"
Get-ChildItem ".\migrations" -Filter "*.sql" |
    Sort-Object Name |
    ForEach-Object { $_.Name } |
    Add-Content $report

Section "GIT STATUS"
git status --short |
    Add-Content $report

Write-Host "Requirement audit: $report"
