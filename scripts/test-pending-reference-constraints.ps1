$ErrorActionPreference = "Stop"

Write-Host ""
Write-Host "========================================"
Write-Host " Pending Reference Constraints Test"
Write-Host "========================================"
Write-Host ""

$walletId = "70000000-0000-0000-0000-000000000001"

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

Run-Sql "DELETE FROM wager_transactions
        WHERE provider_id = 'pending-reference-test-provider';"

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
            'pending-reference-player',
            'BRL',
            10000,
            1,
            NOW(),
            NOW()
        );"

Write-Host "OK - dados preparados."
Write-Host ""

# ------------------------------------------------------------
# PENDING_REFERENCE VALIDO
#
# REFUND chegou antes da BET original.
# Possui:
# - referencia externa
# - next_attempt_at
# - expires_at
# - attempt_count >= 0
# ------------------------------------------------------------

Expect-Success `
    "PENDING_REFERENCE valido" `
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
        reference_attempt_count,
        reference_next_attempt_at,
        reference_expires_at,
        created_at,
        updated_at
    )
    VALUES (
        '71000000-0000-0000-0000-000000000001',
        'refund-pending-valid',
        'pending-reference-test-provider',
        'idem-refund-pending-valid',
        'hash-refund-pending-valid',
        '$walletId',
        'pending-reference-player',
        'round-1',
        'game-1',
        'REFUND',
        2000,
        'BRL',
        'missing-bet-001',
        'PENDING_REFERENCE',
        0,
        '2026-10-02 19:01:00+00',
        '2026-10-02 20:00:00+00',
        '2026-10-02 19:00:00+00',
        '2026-10-02 19:00:00+00'
    );"

# ------------------------------------------------------------
# PENDING_REFERENCE SEM NEXT ATTEMPT
# ------------------------------------------------------------

Expect-Failure `
    "PENDING_REFERENCE sem next_attempt_at" `
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
        reference_attempt_count,
        reference_expires_at,
        created_at,
        updated_at
    )
    VALUES (
        '71000000-0000-0000-0000-000000000002',
        'refund-without-next-attempt',
        'pending-reference-test-provider',
        'idem-refund-without-next-attempt',
        'hash-refund-without-next-attempt',
        '$walletId',
        'pending-reference-player',
        'round-1',
        'game-1',
        'REFUND',
        2000,
        'BRL',
        'missing-bet-002',
        'PENDING_REFERENCE',
        0,
        '2026-10-02 20:00:00+00',
        '2026-10-02 19:00:00+00',
        '2026-10-02 19:00:00+00'
    );"

# ------------------------------------------------------------
# PENDING_REFERENCE SEM EXPIRACAO
# ------------------------------------------------------------

Expect-Failure `
    "PENDING_REFERENCE sem expires_at" `
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
        reference_attempt_count,
        reference_next_attempt_at,
        created_at,
        updated_at
    )
    VALUES (
        '71000000-0000-0000-0000-000000000003',
        'refund-without-expiration',
        'pending-reference-test-provider',
        'idem-refund-without-expiration',
        'hash-refund-without-expiration',
        '$walletId',
        'pending-reference-player',
        'round-1',
        'game-1',
        'REFUND',
        2000,
        'BRL',
        'missing-bet-003',
        'PENDING_REFERENCE',
        0,
        '2026-10-02 19:01:00+00',
        '2026-10-02 19:00:00+00',
        '2026-10-02 19:00:00+00'
    );"

# ------------------------------------------------------------
# EXPIRACAO ANTES DA PROXIMA TENTATIVA
# ------------------------------------------------------------

Expect-Failure `
    "Expiracao anterior ao next_attempt_at" `
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
        reference_attempt_count,
        reference_next_attempt_at,
        reference_expires_at,
        created_at,
        updated_at
    )
    VALUES (
        '71000000-0000-0000-0000-000000000004',
        'refund-invalid-expiration',
        'pending-reference-test-provider',
        'idem-refund-invalid-expiration',
        'hash-refund-invalid-expiration',
        '$walletId',
        'pending-reference-player',
        'round-1',
        'game-1',
        'REFUND',
        2000,
        'BRL',
        'missing-bet-004',
        'PENDING_REFERENCE',
        0,
        '2026-10-02 20:00:00+00',
        '2026-10-02 19:30:00+00',
        '2026-10-02 19:00:00+00',
        '2026-10-02 19:00:00+00'
    );"

# ------------------------------------------------------------
# ATTEMPT COUNT NEGATIVO
# ------------------------------------------------------------

Expect-Failure `
    "Attempt count negativo" `
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
        reference_attempt_count,
        reference_next_attempt_at,
        reference_expires_at,
        created_at,
        updated_at
    )
    VALUES (
        '71000000-0000-0000-0000-000000000005',
        'refund-negative-attempt',
        'pending-reference-test-provider',
        'idem-refund-negative-attempt',
        'hash-refund-negative-attempt',
        '$walletId',
        'pending-reference-player',
        'round-1',
        'game-1',
        'REFUND',
        2000,
        'BRL',
        'missing-bet-005',
        'PENDING_REFERENCE',
        -1,
        '2026-10-02 19:01:00+00',
        '2026-10-02 20:00:00+00',
        '2026-10-02 19:00:00+00',
        '2026-10-02 19:00:00+00'
    );"

# ------------------------------------------------------------
# PENDING_REFERENCE SEM REFERENCIA EXTERNA
#
# Usamos WIN porque REFUND/ROLLBACK ja possuem outra
# constraint que exige referencia.
#
# Assim testamos especificamente:
# wager_transactions_pending_reference_has_reference
# ------------------------------------------------------------

Expect-Failure `
    "PENDING_REFERENCE sem referencia externa" `
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
        reference_attempt_count,
        reference_next_attempt_at,
        reference_expires_at,
        created_at,
        updated_at
    )
    VALUES (
        '71000000-0000-0000-0000-000000000006',
        'win-pending-without-reference',
        'pending-reference-test-provider',
        'idem-win-pending-without-reference',
        'hash-win-pending-without-reference',
        '$walletId',
        'pending-reference-player',
        'round-1',
        'game-1',
        'WIN',
        2000,
        'BRL',
        'PENDING_REFERENCE',
        0,
        '2026-10-02 19:01:00+00',
        '2026-10-02 20:00:00+00',
        '2026-10-02 19:00:00+00',
        '2026-10-02 19:00:00+00'
    );"

# ------------------------------------------------------------
# ESTADO NORMAL NAO PODE CARREGAR SCHEDULE DE RETRY
# ------------------------------------------------------------

Expect-Failure `
    "PENDING com schedule de referencia" `
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
        reference_attempt_count,
        reference_next_attempt_at,
        reference_expires_at,
        created_at,
        updated_at
    )
    VALUES (
        '71000000-0000-0000-0000-000000000007',
        'bet-with-reference-schedule',
        'pending-reference-test-provider',
        'idem-bet-with-reference-schedule',
        'hash-bet-with-reference-schedule',
        '$walletId',
        'pending-reference-player',
        'round-1',
        'game-1',
        'BET',
        2000,
        'BRL',
        'PENDING',
        0,
        '2026-10-02 19:01:00+00',
        '2026-10-02 20:00:00+00',
        '2026-10-02 19:00:00+00',
        '2026-10-02 19:00:00+00'
    );"

# ------------------------------------------------------------
# SIMULA RETRY
#
# Incrementamos contador e movemos next_attempt_at.
# Isso representa o estado persistido que sobrevivera
# ao restart do processo.
# ------------------------------------------------------------

Expect-Success `
    "Persistir nova tentativa" `
    "UPDATE wager_transactions
     SET
        reference_attempt_count =
            reference_attempt_count + 1,
        reference_next_attempt_at =
            '2026-10-02 19:02:00+00',
        updated_at =
            '2026-10-02 19:01:00+00'
     WHERE id =
        '71000000-0000-0000-0000-000000000001';"

# ------------------------------------------------------------
# CONFIRMA CONTADOR PERSISTIDO
# ------------------------------------------------------------

$attemptCount = docker exec jungle-postgres `
    psql `
    -U jungle `
    -d jungle `
    -t `
    -A `
    -c "SELECT reference_attempt_count
        FROM wager_transactions
        WHERE id =
            '71000000-0000-0000-0000-000000000001';"

$attemptCount = $attemptCount.Trim()

if ($attemptCount -ne "1") {
    Write-Host "FALHOU: reference_attempt_count deveria ser 1."
    exit 1
}

Write-Host "OK - attempt count persistido: $attemptCount."
Write-Host ""

# ------------------------------------------------------------
# CONFIRMA QUE CONTINUA PENDING_REFERENCE
# ------------------------------------------------------------

$state = docker exec jungle-postgres `
    psql `
    -U jungle `
    -d jungle `
    -t `
    -A `
    -c "SELECT state
        FROM wager_transactions
        WHERE id =
            '71000000-0000-0000-0000-000000000001';"

$state = $state.Trim()

if ($state -ne "PENDING_REFERENCE") {
    Write-Host "FALHOU: estado deveria continuar PENDING_REFERENCE."
    exit 1
}

Write-Host "OK - estado continua PENDING_REFERENCE."
Write-Host ""

Write-Host "========================================"
Write-Host " TODOS OS TESTES PASSARAM"
Write-Host "========================================"