$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

Write-Host ""
Write-Host "========================================"
Write-Host " #14 ROUND 2"
Write-Host "========================================"

$env:TEST_DATABASE_URL = if ($env:TEST_DATABASE_URL) {
    $env:TEST_DATABASE_URL
} else {
    "postgres://jungle:jungle@localhost:5432/jungle?sslmode=disable"
}

Write-Host ""
Write-Host "=== BASELINE ==="
go test ./... -count=1
if ($LASTEXITCODE -ne 0) { throw "baseline tests failed" }

go vet ./...
if ($LASTEXITCODE -ne 0) { throw "go vet failed" }

Write-Host ""
Write-Host "=== REQUIREMENT EVIDENCE ==="
powershell -ExecutionPolicy Bypass -File .\scripts\audit_14_round2.ps1
if ($LASTEXITCODE -ne 0) { throw "requirement audit failed" }

Write-Host ""
Write-Host "=== RACE DETECTOR ==="
powershell -ExecutionPolicy Bypass -File .\scripts\test_14_race.ps1
if ($LASTEXITCODE -ne 0) { throw "race detector failed" }

Write-Host ""
Write-Host "#14 round 2 automated validation PASS"
Write-Host "Send these files back for the requirement-by-requirement review:"
Write-Host "  race_14_validation.txt"
Write-Host "  requirements_14_round2.txt"
