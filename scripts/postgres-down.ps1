$ErrorActionPreference = "Stop"

Write-Host ""
Write-Host "========================================"
Write-Host " Stopping PostgreSQL"
Write-Host "========================================"
Write-Host ""

docker compose down

if ($LASTEXITCODE -ne 0) {
    Write-Host "ERRO: nao foi possivel parar PostgreSQL."
    exit $LASTEXITCODE
}

Write-Host "PostgreSQL parado."