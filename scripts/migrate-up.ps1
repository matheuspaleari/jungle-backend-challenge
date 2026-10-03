$ErrorActionPreference = "Stop"

Write-Host ""
Write-Host "========================================"
Write-Host " Applying database migrations"
Write-Host "========================================"
Write-Host ""

# ------------------------------------------------------------
# Verifica se o container PostgreSQL esta rodando
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
# Cria tabela de controle das migrations
# ------------------------------------------------------------

Write-Host "Verificando controle de migrations..."

Get-Content -Raw ".\migrations\000000_create_schema_migrations.sql" |
    docker exec -i jungle-postgres `
        psql `
        -v ON_ERROR_STOP=1 `
        -U jungle `
        -d jungle

if ($LASTEXITCODE -ne 0) {
    Write-Host "ERRO: nao foi possivel preparar schema_migrations."
    exit 1
}

Write-Host ""

# ------------------------------------------------------------
# Procura migrations UP
# ------------------------------------------------------------

$migrations = Get-ChildItem `
    -Path ".\migrations" `
    -Filter "*.up.sql" |
    Sort-Object Name

foreach ($migration in $migrations) {

    if ($migration.Name -notmatch '^(\d+)_.*\.up\.sql$') {
        Write-Host "Ignorando arquivo invalido: $($migration.Name)"
        continue
    }

    $version = $Matches[1]

    # --------------------------------------------------------
    # Verifica se a migration ja foi aplicada
    # --------------------------------------------------------

    $alreadyApplied = docker exec jungle-postgres `
        psql `
        -U jungle `
        -d jungle `
        -t `
        -A `
        -c "SELECT EXISTS (
                SELECT 1
                FROM schema_migrations
                WHERE version = '$version'
            );"

    $alreadyApplied = $alreadyApplied.Trim()

    if ($alreadyApplied -eq "t") {
        Write-Host "SKIP: $($migration.Name)"
        continue
    }

    Write-Host "APPLY: $($migration.Name)"

    # --------------------------------------------------------
    # Executa migration + registra versao na mesma transacao
    # --------------------------------------------------------

    $migrationSql = Get-Content -Raw $migration.FullName

    $sql = @"
BEGIN;

$migrationSql

INSERT INTO schema_migrations (
    version
)
VALUES (
    '$version'
);

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
        Write-Host "ERRO na migration: $($migration.Name)"
        Write-Host "A transacao foi revertida."
        exit $exitCode
    }

    Write-Host "OK: $($migration.Name)"
    Write-Host ""
}

Write-Host "========================================"
Write-Host " MIGRATIONS ATUALIZADAS"
Write-Host "========================================"