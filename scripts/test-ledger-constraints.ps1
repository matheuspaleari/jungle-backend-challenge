$ErrorActionPreference = "Stop"

Write-Host ""
Write-Host "========================================"
Write-Host " Ledger Database Constraints Test"
Write-Host "========================================"
Write-Host ""

$walletId = "10000000-0000-0000-0000-000000000001"
$betTransactionId = "20000000-0000-0000-0000-000000000001"
$winTransactionId = "20000000-0000-0000-0000-000000000002"

$validDebitLedgerId = "30000000-0000-0000-0000-000000000001"
$validCreditLedgerId = "30000000-0000-0000-0000-000000000002"

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
        Write-Host "ERRO ao executar SQL de preparacao."
        exit $LASTEXITCODE
    }
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

# ------------------------------------------------------------
# PREPARACAO
# ------------------------------------------------------------

Write-Host "Preparando dados de teste..."

# Ledger e transactions precisam ser removidos antes da wallet.
# Como ledger e append-only, usamos TRUNCATE apenas no ambiente
# controlado deste teste para preparar o fixture.
Run-Sql "TRUNCATE TABLE ledger_entries CASCADE;"

Run-Sql "DELETE FROM wager_transactions
        WHERE id IN (
            '$betTransactionId',
            '$winTransactionId'
        );"

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
            'ledger-test-player',
            'BRL',
            10000,
            1,
            NOW(),
            NOW()
        );"

Run-Sql "INSERT INTO wager_transactions (
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
            result_balance_minor,
            created_at,
            updated_at
        )
        VALUES (
            '$betTransactionId',
            'ledger-test-bet',
            'ledger-test-provider',
            'ledger-test-idem-bet',
            'ledger-test-hash-bet',
            '$walletId',
            'ledger-test-player',
            'ledger-test-round',
            'ledger-test-game',
            'BET',
            2000,
            'BRL',
            'PROCESSED',
            8000,
            NOW(),
            NOW()
        );"

Run-Sql "INSERT INTO wager_transactions (
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
            result_balance_minor,
            created_at,
            updated_at
        )
        VALUES (
            '$winTransactionId',
            'ledger-test-win',
            'ledger-test-provider',
            'ledger-test-idem-win',
            'ledger-test-hash-win',
            '$walletId',
            'ledger-test-player',
            'ledger-test-round',
            'ledger-test-game',
            'WIN',
            3000,
            'BRL',
            'PROCESSED',
            11000,
            NOW(),
            NOW()
        );"

Write-Host "OK - dados preparados."
Write-Host ""

# ------------------------------------------------------------
# DEBIT VALIDO
# 100.00 -> 80.00
# ------------------------------------------------------------

Expect-Success `
    "DEBIT valido" `
    "INSERT INTO ledger_entries (
        id,
        wallet_id,
        transaction_id,
        direction,
        value_minor,
        currency,
        balance_before_minor,
        balance_after_minor,
        created_at
    )
    VALUES (
        '$validDebitLedgerId',
        '$walletId',
        '$betTransactionId',
        'DEBIT',
        2000,
        'BRL',
        10000,
        8000,
        NOW()
    );"

# ------------------------------------------------------------
# CREDIT VALIDO
# 80.00 -> 110.00
# ------------------------------------------------------------

Expect-Success `
    "CREDIT valido" `
    "INSERT INTO ledger_entries (
        id,
        wallet_id,
        transaction_id,
        direction,
        value_minor,
        currency,
        balance_before_minor,
        balance_after_minor,
        created_at
    )
    VALUES (
        '$validCreditLedgerId',
        '$walletId',
        '$winTransactionId',
        'CREDIT',
        3000,
        'BRL',
        8000,
        11000,
        NOW()
    );"

# ------------------------------------------------------------
# RESULTADO MATEMATICAMENTE INCORRETO
# ------------------------------------------------------------

Expect-Failure `
    "Movimento matematicamente incorreto" `
    "INSERT INTO ledger_entries (
        id,
        wallet_id,
        transaction_id,
        direction,
        value_minor,
        currency,
        balance_before_minor,
        balance_after_minor,
        created_at
    )
    VALUES (
        '30000000-0000-0000-0000-000000000003',
        '$walletId',
        '$betTransactionId',
        'DEBIT',
        2000,
        'BRL',
        10000,
        9000,
        NOW()
    );"

# ------------------------------------------------------------
# SALDO FINAL NEGATIVO
# ------------------------------------------------------------

Expect-Failure `
    "Saldo negativo" `
    "INSERT INTO ledger_entries (
        id,
        wallet_id,
        transaction_id,
        direction,
        value_minor,
        currency,
        balance_before_minor,
        balance_after_minor,
        created_at
    )
    VALUES (
        '30000000-0000-0000-0000-000000000004',
        '$walletId',
        '$betTransactionId',
        'DEBIT',
        12000,
        'BRL',
        10000,
        -2000,
        NOW()
    );"

# ------------------------------------------------------------
# MOVIMENTO DUPLICADO
# ------------------------------------------------------------

Expect-Failure `
    "Mesmo wallet + transaction duplicado" `
    "INSERT INTO ledger_entries (
        id,
        wallet_id,
        transaction_id,
        direction,
        value_minor,
        currency,
        balance_before_minor,
        balance_after_minor,
        created_at
    )
    VALUES (
        '30000000-0000-0000-0000-000000000005',
        '$walletId',
        '$betTransactionId',
        'DEBIT',
        2000,
        'BRL',
        10000,
        8000,
        NOW()
    );"

# ------------------------------------------------------------
# UPDATE PROIBIDO
# ------------------------------------------------------------

Expect-Failure `
    "UPDATE de ledger" `
    "UPDATE ledger_entries
     SET value_minor = 1000
     WHERE id = '$validDebitLedgerId';"

# ------------------------------------------------------------
# DELETE PROIBIDO
# ------------------------------------------------------------

Expect-Failure `
    "DELETE de ledger" `
    "DELETE FROM ledger_entries
     WHERE id = '$validDebitLedgerId';"

# ------------------------------------------------------------
# GARANTE QUE OS REGISTROS ORIGINAIS CONTINUAM PRESENTES
# ------------------------------------------------------------

$count = docker exec jungle-postgres `
    psql `
    -U jungle `
    -d jungle `
    -t `
    -A `
    -c "SELECT COUNT(*)
        FROM ledger_entries
        WHERE id IN (
            '$validDebitLedgerId',
            '$validCreditLedgerId'
        );"

$count = $count.Trim()

if ($count -ne "2") {
    Write-Host "FALHOU: ledger foi alterado durante os testes."
    exit 1
}

Write-Host "OK - registros originais permanecem imutaveis."
Write-Host ""

Write-Host "========================================"
Write-Host " TODOS OS TESTES PASSARAM"
Write-Host "========================================"