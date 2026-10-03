$ErrorActionPreference = "Stop"

Write-Host ""
Write-Host "========================================"
Write-Host " PostgreSQL Go Integration Tests"
Write-Host "========================================"
Write-Host ""

$running = docker inspect `
    --format="{{.State.Running}}" `
    jungle-postgres 2>$null

if ($running -ne "true") {
    Write-Host "ERRO: PostgreSQL nao esta rodando."
    Write-Host ""
    Write-Host "Execute primeiro:"
    Write-Host ".\scripts\postgres-up.ps1"
    exit 1
}

$previousDatabaseURL = $env:TEST_DATABASE_URL

try {
    $env:TEST_DATABASE_URL = `
        "postgres://jungle:jungle@localhost:5432/jungle?sslmode=disable"

    Write-Host "Executando testes de integracao PostgreSQL..."
    Write-Host ""

    go test `
        ./internal/infrastructure/postgres `
        -count=1 `
        -v

    if ($LASTEXITCODE -ne 0) {
        Write-Host ""
        Write-Host "ERRO: testes de integracao PostgreSQL falharam."
        exit $LASTEXITCODE
    }

    Write-Host ""
    Write-Host "========================================"
    Write-Host " POSTGRESQL VALIDADO"
    Write-Host "========================================"
}
finally {
    $env:TEST_DATABASE_URL = $previousDatabaseURL
}