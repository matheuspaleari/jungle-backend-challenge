$ErrorActionPreference = "Stop"

Write-Host ""
Write-Host "========================================"
Write-Host " Starting PostgreSQL"
Write-Host "========================================"
Write-Host ""

docker compose up -d postgres

if ($LASTEXITCODE -ne 0) {
    Write-Host "ERRO: nao foi possivel iniciar PostgreSQL."
    exit $LASTEXITCODE
}

Write-Host ""
Write-Host "Aguardando PostgreSQL ficar healthy..."

$maxAttempts = 30

for ($attempt = 1; $attempt -le $maxAttempts; $attempt++) {
    $status = docker inspect `
        --format="{{.State.Health.Status}}" `
        jungle-postgres 2>$null

    if ($status -eq "healthy") {
        Write-Host ""
        Write-Host "PostgreSQL esta pronto."
        exit 0
    }

    Write-Host "Tentativa $attempt/$maxAttempts - status: $status"
    Start-Sleep -Seconds 2
}

Write-Host ""
Write-Host "ERRO: PostgreSQL nao ficou healthy."
docker compose logs postgres

exit 1