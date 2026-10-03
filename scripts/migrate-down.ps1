$ErrorActionPreference = "Stop"

Write-Host ""
Write-Host "========================================"
Write-Host " Reverting last database migration"
Write-Host "========================================"
Write-Host ""

# ------------------------------------------------------------
# Verifica se PostgreSQL esta rodando
# ------------------------------------------------------------

$running = docker inspect `
    --format="{{.State.Running}}" `
    jungle-postgres 2>$null

if ($running -ne "true") {
    Write-Host "ERRO: PostgreSQL nao esta rodando."
    Write-Host "Execute primeiro:"
    Write-Host ".\scripts\postgres-up.ps1"
    exit 1
}

# ------------------------------------------------------------
# Verifica se schema_migrations existe
# ------------------------------------------------------------

$tableExists = docker exec jungle-postgres `
    psql `
    -U jungle `
    -d jungle `
    -t `
    -A `
    -c "SELECT to_regclass('public.schema_migrations') IS NOT NULL;"

$tableExists = $tableExists.Trim()

if ($tableExists -ne "t") {
    Write-Host "Nenhuma migration registrada."
    exit 0
}

# ------------------------------------------------------------
# Descobre a ultima migration aplicada
# ------------------------------------------------------------

$version = docker exec jungle-postgres `
    psql `
    -U jungle `
    -d jungle `
    -t `
    -A `
    -c "SELECT version
        FROM schema_migrations
        ORDER BY version DESC
        LIMIT 1;"

$version = $version.Trim()

if ([string]::IsNullOrWhiteSpace($version)) {
    Write-Host "Nenhuma migration para reverter."
    exit 0
}

# ------------------------------------------------------------
# Localiza arquivo DOWN correspondente
# ------------------------------------------------------------

$migration = Get-ChildItem `
    -Path ".\migrations" `
    -Filter "$version`_*.down.sql" |
    Select-Object -First 1

if ($null -eq $migration) {
    Write-Host "ERRO: migration DOWN nao encontrada para:"
    Write-Host $version
    exit 1
}

Write-Host "REVERT: $($migration.Name)"
Write-Host ""

$migrationSql = Get-Content -Raw $migration.FullName

# ------------------------------------------------------------
# Migration DOWN + remocao do registro sao atomicas
# ------------------------------------------------------------

$sql = @"
BEGIN;

$migrationSql

DELETE FROM schema_migrations
WHERE version = '$version';

COMMIT;
"@

$previousErrorActionPreference = $ErrorActionPreference
$ErrorActionPreference = "Continue"

$sql |
    docker exec -i jungle-postgres `
        psql `
        -v ON_ERROR_STOP=1 `
        -U jungle `
        -d jungle

$exitCode = $LASTEXITCODE

$ErrorActionPreference = $previousErrorActionPreference

if ($exitCode -ne 0) {
    Write-Host ""
    Write-Host "ERRO ao reverter migration:"
    Write-Host $migration.Name
    Write-Host ""
    Write-Host "A transacao foi revertida."
    exit $exitCode
}

Write-Host ""
Write-Host "OK: migration $version revertida."
Write-Host ""
Write-Host "========================================"
Write-Host " ROLLBACK CONCLUIDO"
Write-Host "========================================"