$ErrorActionPreference = "Stop"

Write-Host ""
Write-Host "========================================"
Write-Host " Wallet Database Constraints Test"
Write-Host "========================================"
Write-Host ""

function Expect-Failure {
    param (
        [string]$Name,
        [string]$Sql
    )

    Write-Host "TEST: $Name"

    # Estes comandos DEVEM falhar.
    # Evitamos transformar o stderr do docker em erro fatal do PowerShell.
    $previousErrorActionPreference = $ErrorActionPreference
    $ErrorActionPreference = "Continue"

    $output = docker exec jungle-postgres `
        psql `
        -U jungle `
        -d jungle `
        -v ON_ERROR_STOP=1 `
        -c $Sql 2>&1

    $exitCode = $LASTEXITCODE

    $ErrorActionPreference = $previousErrorActionPreference

    if ($exitCode -eq 0) {
        Write-Host "FALHOU: banco aceitou uma operacao invalida."
        Write-Host $output
        exit 1
    }

    Write-Host "OK - PostgreSQL rejeitou corretamente."
    Write-Host ""
}

# Limpa registros de execucoes anteriores.
docker exec jungle-postgres `
    psql `
    -U jungle `
    -d jungle `
    -v ON_ERROR_STOP=1 `
    -c "DELETE FROM wallets WHERE player_id LIKE 'constraint-test-%';" |
    Out-Null

if ($LASTEXITCODE -ne 0) {
    Write-Host "FALHOU: nao foi possivel preparar os testes."
    exit 1
}

Write-Host "Criando wallet valida..."

docker exec jungle-postgres `
    psql `
    -U jungle `
    -d jungle `
    -v ON_ERROR_STOP=1 `
    -c "INSERT INTO wallets (
            id,
            player_id,
            currency,
            balance_minor,
            version,
            created_at,
            updated_at
        ) VALUES (
            '00000000-0000-0000-0000-000000000001',
            'constraint-test-player',
            'BRL',
            10000,
            1,
            NOW(),
            NOW()
        );"

if ($LASTEXITCODE -ne 0) {
    Write-Host "FALHOU: wallet valida foi rejeitada."
    exit 1
}

Write-Host "OK - wallet valida aceita."
Write-Host ""

Expect-Failure `
    "Saldo negativo" `
    "INSERT INTO wallets (
        id, player_id, currency, balance_minor,
        version, created_at, updated_at
    ) VALUES (
        '00000000-0000-0000-0000-000000000002',
        'constraint-test-negative',
        'BRL',
        -1,
        1,
        NOW(),
        NOW()
    );"

Expect-Failure `
    "Player + moeda duplicados" `
    "INSERT INTO wallets (
        id, player_id, currency, balance_minor,
        version, created_at, updated_at
    ) VALUES (
        '00000000-0000-0000-0000-000000000003',
        'constraint-test-player',
        'BRL',
        5000,
        1,
        NOW(),
        NOW()
    );"

Expect-Failure `
    "Versao zero" `
    "INSERT INTO wallets (
        id, player_id, currency, balance_minor,
        version, created_at, updated_at
    ) VALUES (
        '00000000-0000-0000-0000-000000000004',
        'constraint-test-version',
        'BRL',
        1000,
        0,
        NOW(),
        NOW()
    );"

Expect-Failure `
    "Moeda invalida" `
    "INSERT INTO wallets (
        id, player_id, currency, balance_minor,
        version, created_at, updated_at
    ) VALUES (
        '00000000-0000-0000-0000-000000000005',
        'constraint-test-currency',
        'brl',
        1000,
        1,
        NOW(),
        NOW()
    );"

Expect-Failure `
    "Player vazio" `
    "INSERT INTO wallets (
        id, player_id, currency, balance_minor,
        version, created_at, updated_at
    ) VALUES (
        '00000000-0000-0000-0000-000000000006',
        '',
        'BRL',
        1000,
        1,
        NOW(),
        NOW()
    );"

Write-Host "========================================"
Write-Host " TODOS OS TESTES PASSARAM"
Write-Host "========================================"