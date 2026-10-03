$ErrorActionPreference = "Stop"

Write-Host ""
Write-Host "========================================"
Write-Host " Wager Reference Constraints Test"
Write-Host "========================================"
Write-Host ""

$walletId = "40000000-0000-0000-0000-000000000001"

function Run-Sql {
    param (
        [string]$Sql
    )

    docker exec jungle-postgres `
        psql `
        -U jungle `
        -d jungle `
        -v ON_ERROR_STOP=1 `
        -c $Sql

    if ($LASTEXITCODE -ne 0) {
        Write-Host "ERRO ao preparar dados de teste."
        exit $LASTEXITCODE
    }
}

function Expect-Success {
    param (
        [string]$Name,
        [string]$Sql
    )

    Write-Host "TEST: $Name"

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

    if ($exitCode -ne 0) {
        Write-Host "FALHOU: PostgreSQL rejeitou uma operacao valida."
        Write-Host $output
        exit 1
    }

    Write-Host "OK - PostgreSQL aceitou corretamente."
    Write-Host ""
}

function Expect-Failure {
    param (
        [string]$Name,
        [string]$Sql
    )

    Write-Host "TEST: $Name"

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
        Write-Host "FALHOU: PostgreSQL aceitou uma operacao invalida."
        Write-Host $output
        exit 1
    }

    Write-Host "OK - PostgreSQL rejeitou corretamente."
    Write-Host ""
}

# ------------------------------------------------------------
# PREPARACAO
# ------------------------------------------------------------

Write-Host "Preparando dados de teste..."

Run-Sql "DELETE FROM wallets
        WHERE id = '$walletId';"

Run-Sql "INSERT INTO wallets (
            id,
            player_id,
            currency,
            balance_minor,
            version,
            created_at,
            updated_at
        )
        VALUES (
            '$walletId',
            'wager-reference-player',
            'BRL',
            10000,
            1,
            NOW(),
            NOW()
        );"

Write-Host "OK - dados preparados."
Write-Host ""

# ------------------------------------------------------------
# WIN SEM REFERENCIA
# Deve ser permitido.
# ------------------------------------------------------------

Expect-Success `
    "WIN sem referencia" `
    "INSERT INTO wager_transactions (
        id,
        external_transaction_id,
        provider_id,
        idempotency_key,
        payload_hash,
        wallet_id,
        player_id,
        round_id,
        game_id,
        kind,
        amount_minor,
        currency,
        state,
        created_at,
        updated_at
    )
    VALUES (
        '41000000-0000-0000-0000-000000000001',
        'win-without-reference',
        'reference-test-provider',
        'idem-win-without-reference',
        'hash-win-without-reference',
        '$walletId',
        'wager-reference-player',
        'round-1',
        'game-1',
        'WIN',
        1000,
        'BRL',
        'PENDING',
        NOW(),
        NOW()
    );"

# ------------------------------------------------------------
# WIN COM REFERENCIA EXTERNA
# Deve ser permitido.
# ------------------------------------------------------------

Expect-Success `
    "WIN com referencia" `
    "INSERT INTO wager_transactions (
        id,
        external_transaction_id,
        provider_id,
        idempotency_key,
        payload_hash,
        wallet_id,
        player_id,
        round_id,
        game_id,
        kind,
        amount_minor,
        currency,
        reference_external_transaction_id,
        state,
        created_at,
        updated_at
    )
    VALUES (
        '41000000-0000-0000-0000-000000000002',
        'win-with-reference',
        'reference-test-provider',
        'idem-win-with-reference',
        'hash-win-with-reference',
        '$walletId',
        'wager-reference-player',
        'round-1',
        'game-1',
        'WIN',
        1000,
        'BRL',
        'original-bet',
        'PENDING_REFERENCE',
        NOW(),
        NOW()
    );"

# ------------------------------------------------------------
# REFUND SEM REFERENCIA
# Deve ser rejeitado.
# ------------------------------------------------------------

Expect-Failure `
    "REFUND sem referencia" `
    "INSERT INTO wager_transactions (
        id,
        external_transaction_id,
        provider_id,
        idempotency_key,
        payload_hash,
        wallet_id,
        player_id,
        round_id,
        game_id,
        kind,
        amount_minor,
        currency,
        state,
        created_at,
        updated_at
    )
    VALUES (
        '41000000-0000-0000-0000-000000000003',
        'refund-without-reference',
        'reference-test-provider',
        'idem-refund-without-reference',
        'hash-refund-without-reference',
        '$walletId',
        'wager-reference-player',
        'round-1',
        'game-1',
        'REFUND',
        1000,
        'BRL',
        'PENDING',
        NOW(),
        NOW()
    );"

# ------------------------------------------------------------
# ROLLBACK SEM REFERENCIA
# Deve ser rejeitado.
# ------------------------------------------------------------

Expect-Failure `
    "ROLLBACK sem referencia" `
    "INSERT INTO wager_transactions (
        id,
        external_transaction_id,
        provider_id,
        idempotency_key,
        payload_hash,
        wallet_id,
        player_id,
        round_id,
        game_id,
        kind,
        amount_minor,
        currency,
        state,
        created_at,
        updated_at
    )
    VALUES (
        '41000000-0000-0000-0000-000000000004',
        'rollback-without-reference',
        'reference-test-provider',
        'idem-rollback-without-reference',
        'hash-rollback-without-reference',
        '$walletId',
        'wager-reference-player',
        'round-1',
        'game-1',
        'ROLLBACK',
        1000,
        'BRL',
        'PENDING',
        NOW(),
        NOW()
    );"

# ------------------------------------------------------------
# BET COM REFERENCIA
# Deve ser rejeitado.
# ------------------------------------------------------------

Expect-Failure `
    "BET com referencia" `
    "INSERT INTO wager_transactions (
        id,
        external_transaction_id,
        provider_id,
        idempotency_key,
        payload_hash,
        wallet_id,
        player_id,
        round_id,
        game_id,
        kind,
        amount_minor,
        currency,
        reference_external_transaction_id,
        state,
        created_at,
        updated_at
    )
    VALUES (
        '41000000-0000-0000-0000-000000000005',
        'bet-with-reference',
        'reference-test-provider',
        'idem-bet-with-reference',
        'hash-bet-with-reference',
        '$walletId',
        'wager-reference-player',
        'round-1',
        'game-1',
        'BET',
        1000,
        'BRL',
        'another-transaction',
        'PENDING',
        NOW(),
        NOW()
    );"

# ------------------------------------------------------------
# LOSS COM REFERENCIA
# Deve ser rejeitado.
# ------------------------------------------------------------

Expect-Failure `
    "LOSS com referencia" `
    "INSERT INTO wager_transactions (
        id,
        external_transaction_id,
        provider_id,
        idempotency_key,
        payload_hash,
        wallet_id,
        player_id,
        round_id,
        game_id,
        kind,
        amount_minor,
        currency,
        reference_external_transaction_id,
        state,
        created_at,
        updated_at
    )
    VALUES (
        '41000000-0000-0000-0000-000000000006',
        'loss-with-reference',
        'reference-test-provider',
        'idem-loss-with-reference',
        'hash-loss-with-reference',
        '$walletId',
        'wager-reference-player',
        'round-1',
        'game-1',
        'LOSS',
        0,
        'BRL',
        'another-transaction',
        'PENDING',
        NOW(),
        NOW()
    );"

# ------------------------------------------------------------
# AUTOREFERENCIA INTERNA
#
# Primeiro criamos um WIN valido.
# Depois tentamos fazer resolved_reference_transaction_id
# apontar para o proprio ID.
#
# Como UPDATE normal seria permitido nesta tabela, quem deve
# bloquear esta operacao e a constraint no_self_reference.
# ------------------------------------------------------------

Expect-Success `
    "Criar WIN para teste de autorreferencia" `
    "INSERT INTO wager_transactions (
        id,
        external_transaction_id,
        provider_id,
        idempotency_key,
        payload_hash,
        wallet_id,
        player_id,
        round_id,
        game_id,
        kind,
        amount_minor,
        currency,
        state,
        created_at,
        updated_at
    )
    VALUES (
        '41000000-0000-0000-0000-000000000007',
        'win-self-reference',
        'reference-test-provider',
        'idem-win-self-reference',
        'hash-win-self-reference',
        '$walletId',
        'wager-reference-player',
        'round-1',
        'game-1',
        'WIN',
        1000,
        'BRL',
        'PENDING',
        NOW(),
        NOW()
    );"

Expect-Failure `
    "Transacao nao pode referenciar ela mesma" `
    "UPDATE wager_transactions
     SET
        resolved_reference_transaction_id =
            '41000000-0000-0000-0000-000000000007',
        updated_at = NOW()
     WHERE id =
        '41000000-0000-0000-0000-000000000007';"

Write-Host "========================================"
Write-Host " TODOS OS TESTES PASSARAM"
Write-Host "========================================"