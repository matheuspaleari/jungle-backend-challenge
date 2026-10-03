$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$report = Join-Path $root "audit_14_detailed.txt"
$reportFullPath = [System.IO.Path]::GetFullPath($report)

"=== #14 FINAL AUDIT ===" | Set-Content $report
"Generated: $(Get-Date -Format o)" | Add-Content $report

function Add-Matches([string]$Title, [string[]]$Roots, [string]$Pattern) {
    "`n=== $Title ===" | Add-Content $report

    Get-ChildItem $Roots -Recurse -File -ErrorAction SilentlyContinue |
        Where-Object {
            [System.IO.Path]::GetFullPath($_.FullName) -ne $reportFullPath -and
            $_.FullName -notmatch '\\.git\\|\\.tmp-' -and
            $_.Extension -ne ".exe"
        } |
        Select-String -Pattern $Pattern -ErrorAction SilentlyContinue |
        ForEach-Object {
            "$($_.Path):$($_.LineNumber): $($_.Line.Trim())"
        } |
        Add-Content $report
}

"`n=== GIT STATUS ===" | Add-Content $report
git status --short | Out-String -Width 500 | Add-Content $report

"`n=== GITIGNORE ===" | Add-Content $report
Get-Content ".\.gitignore" | Add-Content $report

Add-Matches "PENDING REFERENCE" @(".\internal") "PendingReferenceWorker|NewPendingReferenceWorker|RunOnce|pendingReference"
Add-Matches "WALLET CONSTRAINTS" @(".\migrations") "wallets_player_currency_unique|wallets_provider_player_currency_unique|UNIQUE"
Add-Matches "POSSIBLE SECRETS" @(".") "password|secret|client_secret|PRIVATE KEY|BEGIN RSA|BEGIN OPENSSH|api[_-]?key"
Add-Matches "TODO / FIXME / HACK" @(".\internal", ".\cmd") "TODO|FIXME|HACK|XXX"

"`n=== GENERATED FILES STILL PRESENT ===" | Add-Content $report
Get-ChildItem -Recurse -Force -File |
    Where-Object {
        $_.FullName -notmatch '\\.git\\' -and
        [System.IO.Path]::GetFullPath($_.FullName) -ne $reportFullPath -and (
            $_.Extension -in @(".exe",".log",".tmp",".out") -or
            $_.Name -match "validation|e2e|audit|report"
        )
    } |
    ForEach-Object {
        "$($_.FullName) | $($_.Length) bytes"
    } |
    Add-Content $report

Write-Host "Audit report: $reportFullPath"
