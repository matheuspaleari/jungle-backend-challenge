$ErrorActionPreference = "Stop"

Write-Host ""
Write-Host "========================================"
Write-Host " Outbox Database Constraints Test"
Write-Host "========================================"
Write-Host ""

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

Run-Sql "DELETE FROM outbox_events
        WHERE correlation_id LIKE 'outbox-test-%';"

Write-Host "OK - dados preparados."
Write-Host ""

# ------------------------------------------------------------
# EVENTO PENDENTE VALIDO
#
# jsonb_build_object evita problemas de escaping entre
# PowerShell -> Docker -> psql.
#
# O dinheiro continua representado como STRING:
# amount = '25.00'
# ------------------------------------------------------------

Expect-Success `
    "Evento pendente valido" `
    "INSERT INTO outbox_events (
        id,
        event_type,
        aggregate_id,
        correlation_id,
        occurred_at,
        event_version,
        payload,
        retry_count,
        next_attempt_at,
        created_at
    )
    VALUES (
        '60000000-0000-0000-0000-000000000001',
        'WagerTransactionProcessed',
        'transaction-001',
        'outbox-test-correlation-001',
        '2026-01-01 10:00:00+00',
        1,
        jsonb_build_object(
            'transactionId',
            'transaction-001',
            'money',
            jsonb_build_object(
                'amount',
                '25.00',
                'currency',
                'BRL'
            )
        ),
        0,
        '2026-01-01 10:00:00+00',
        '2026-01-01 10:00:00+00'
    );"

# ------------------------------------------------------------
# EVENT TYPE VAZIO
# ------------------------------------------------------------

Expect-Failure `
    "Event type vazio" `
    "INSERT INTO outbox_events (
        id,
        event_type,
        aggregate_id,
        correlation_id,
        occurred_at,
        event_version,
        payload,
        next_attempt_at,
        created_at
    )
    VALUES (
        '60000000-0000-0000-0000-000000000002',
        '',
        'transaction-002',
        'outbox-test-correlation-002',
        '2026-01-01 10:00:00+00',
        1,
        jsonb_build_object(),
        '2026-01-01 10:00:00+00',
        '2026-01-01 10:00:00+00'
    );"

# ------------------------------------------------------------
# AGGREGATE ID VAZIO
# ------------------------------------------------------------

Expect-Failure `
    "Aggregate ID vazio" `
    "INSERT INTO outbox_events (
        id,
        event_type,
        aggregate_id,
        correlation_id,
        occurred_at,
        event_version,
        payload,
        next_attempt_at,
        created_at
    )
    VALUES (
        '60000000-0000-0000-0000-000000000003',
        'WagerTransactionProcessed',
        '',
        'outbox-test-correlation-003',
        '2026-01-01 10:00:00+00',
        1,
        jsonb_build_object(),
        '2026-01-01 10:00:00+00',
        '2026-01-01 10:00:00+00'
    );"

# ------------------------------------------------------------
# CORRELATION ID VAZIO
# ------------------------------------------------------------

Expect-Failure `
    "Correlation ID vazio" `
    "INSERT INTO outbox_events (
        id,
        event_type,
        aggregate_id,
        correlation_id,
        occurred_at,
        event_version,
        payload,
        next_attempt_at,
        created_at
    )
    VALUES (
        '60000000-0000-0000-0000-000000000004',
        'WagerTransactionProcessed',
        'transaction-004',
        '',
        '2026-01-01 10:00:00+00',
        1,
        jsonb_build_object(),
        '2026-01-01 10:00:00+00',
        '2026-01-01 10:00:00+00'
    );"

# ------------------------------------------------------------
# CAUSATION ID VAZIO
# ------------------------------------------------------------

Expect-Failure `
    "Causation ID vazio" `
    "INSERT INTO outbox_events (
        id,
        event_type,
        aggregate_id,
        correlation_id,
        causation_id,
        occurred_at,
        event_version,
        payload,
        next_attempt_at,
        created_at
    )
    VALUES (
        '60000000-0000-0000-0000-000000000005',
        'WagerTransactionProcessed',
        'transaction-005',
        'outbox-test-correlation-005',
        '',
        '2026-01-01 10:00:00+00',
        1,
        jsonb_build_object(),
        '2026-01-01 10:00:00+00',
        '2026-01-01 10:00:00+00'
    );"

# ------------------------------------------------------------
# VERSAO ZERO
# ------------------------------------------------------------

Expect-Failure `
    "Event version zero" `
    "INSERT INTO outbox_events (
        id,
        event_type,
        aggregate_id,
        correlation_id,
        occurred_at,
        event_version,
        payload,
        next_attempt_at,
        created_at
    )
    VALUES (
        '60000000-0000-0000-0000-000000000006',
        'WagerTransactionProcessed',
        'transaction-006',
        'outbox-test-correlation-006',
        '2026-01-01 10:00:00+00',
        0,
        jsonb_build_object(),
        '2026-01-01 10:00:00+00',
        '2026-01-01 10:00:00+00'
    );"

# ------------------------------------------------------------
# PAYLOAD PRECISA SER OBJETO JSON
#
# jsonb_build_array cria propositalmente um array,
# que deve ser rejeitado pela constraint.
# ------------------------------------------------------------

Expect-Failure `
    "Payload JSON array" `
    "INSERT INTO outbox_events (
        id,
        event_type,
        aggregate_id,
        correlation_id,
        occurred_at,
        event_version,
        payload,
        next_attempt_at,
        created_at
    )
    VALUES (
        '60000000-0000-0000-0000-000000000007',
        'WagerTransactionProcessed',
        'transaction-007',
        'outbox-test-correlation-007',
        '2026-01-01 10:00:00+00',
        1,
        jsonb_build_array(
            'invalid-payload'
        ),
        '2026-01-01 10:00:00+00',
        '2026-01-01 10:00:00+00'
    );"

# ------------------------------------------------------------
# RETRY COUNT NEGATIVO
# ------------------------------------------------------------

Expect-Failure `
    "Retry count negativo" `
    "INSERT INTO outbox_events (
        id,
        event_type,
        aggregate_id,
        correlation_id,
        occurred_at,
        event_version,
        payload,
        retry_count,
        next_attempt_at,
        created_at
    )
    VALUES (
        '60000000-0000-0000-0000-000000000008',
        'WagerTransactionProcessed',
        'transaction-008',
        'outbox-test-correlation-008',
        '2026-01-01 10:00:00+00',
        1,
        jsonb_build_object(),
        -1,
        '2026-01-01 10:00:00+00',
        '2026-01-01 10:00:00+00'
    );"

# ------------------------------------------------------------
# NEXT ATTEMPT ANTERIOR AO EVENTO
# ------------------------------------------------------------

Expect-Failure `
    "Next attempt anterior ao occurredAt" `
    "INSERT INTO outbox_events (
        id,
        event_type,
        aggregate_id,
        correlation_id,
        occurred_at,
        event_version,
        payload,
        next_attempt_at,
        created_at
    )
    VALUES (
        '60000000-0000-0000-0000-000000000009',
        'WagerTransactionProcessed',
        'transaction-009',
        'outbox-test-correlation-009',
        '2026-01-01 10:00:00+00',
        1,
        jsonb_build_object(),
        '2026-01-01 09:59:59+00',
        '2026-01-01 10:00:00+00'
    );"

# ------------------------------------------------------------
# CREATED AT ANTERIOR AO EVENTO
# ------------------------------------------------------------

Expect-Failure `
    "Created at anterior ao occurredAt" `
    "INSERT INTO outbox_events (
        id,
        event_type,
        aggregate_id,
        correlation_id,
        occurred_at,
        event_version,
        payload,
        next_attempt_at,
        created_at
    )
    VALUES (
        '60000000-0000-0000-0000-000000000010',
        'WagerTransactionProcessed',
        'transaction-010',
        'outbox-test-correlation-010',
        '2026-01-01 10:00:00+00',
        1,
        jsonb_build_object(),
        '2026-01-01 10:00:00+00',
        '2026-01-01 09:59:59+00'
    );"

# ------------------------------------------------------------
# PUBLISHED AT ANTERIOR AO EVENTO
# ------------------------------------------------------------

Expect-Failure `
    "Published at anterior ao occurredAt" `
    "INSERT INTO outbox_events (
        id,
        event_type,
        aggregate_id,
        correlation_id,
        occurred_at,
        event_version,
        payload,
        next_attempt_at,
        published_at,
        created_at
    )
    VALUES (
        '60000000-0000-0000-0000-000000000011',
        'WagerTransactionProcessed',
        'transaction-011',
        'outbox-test-correlation-011',
        '2026-01-01 10:00:00+00',
        1,
        jsonb_build_object(),
        '2026-01-01 10:00:00+00',
        '2026-01-01 09:59:59+00',
        '2026-01-01 10:00:00+00'
    );"

# ------------------------------------------------------------
# SIMULA FALHA DE PUBLICACAO
#
# O mesmo event ID deve continuar persistido.
# Apenas retry_count e next_attempt_at mudam.
# ------------------------------------------------------------

Expect-Success `
    "Registrar retry de publicacao" `
    "UPDATE outbox_events
     SET
        retry_count = retry_count + 1,
        next_attempt_at = '2026-01-01 10:01:00+00'
     WHERE id =
        '60000000-0000-0000-0000-000000000001';"

# ------------------------------------------------------------
# CONFIRMA EVENT ID ESTAVEL
# ------------------------------------------------------------

$eventId = docker exec jungle-postgres `
    psql `
    -U jungle `
    -d jungle `
    -t `
    -A `
    -c "SELECT id
        FROM outbox_events
        WHERE correlation_id =
            'outbox-test-correlation-001';"

$eventId = $eventId.Trim()

if ($eventId -ne "60000000-0000-0000-0000-000000000001") {
    Write-Host "FALHOU: event ID mudou durante retry."
    exit 1
}

Write-Host "OK - event ID permaneceu estavel durante retry."
Write-Host ""

# ------------------------------------------------------------
# CONFIRMA RETRY COUNT
# ------------------------------------------------------------

$retryCount = docker exec jungle-postgres `
    psql `
    -U jungle `
    -d jungle `
    -t `
    -A `
    -c "SELECT retry_count
        FROM outbox_events
        WHERE id =
            '60000000-0000-0000-0000-000000000001';"

$retryCount = $retryCount.Trim()

if ($retryCount -ne "1") {
    Write-Host "FALHOU: retry_count deveria ser 1."
    exit 1
}

Write-Host "OK - retry_count persistido."
Write-Host ""

# ------------------------------------------------------------
# CONFIRMA MONEY COMO STRING NO JSON
# ------------------------------------------------------------

$amount = docker exec jungle-postgres `
    psql `
    -U jungle `
    -d jungle `
    -t `
    -A `
    -c "SELECT payload->'money'->>'amount'
        FROM outbox_events
        WHERE id =
            '60000000-0000-0000-0000-000000000001';"

$amount = $amount.Trim()

if ($amount -ne "25.00") {
    Write-Host "FALHOU: amount nao foi persistido corretamente."
    exit 1
}

Write-Host "OK - Money persistido como decimal string: $amount."
Write-Host ""

# ------------------------------------------------------------
# SIMULA PUBLICACAO COM SUCESSO
# ------------------------------------------------------------

Expect-Success `
    "Marcar evento como publicado" `
    "UPDATE outbox_events
     SET published_at = '2026-01-01 10:02:00+00'
     WHERE id =
        '60000000-0000-0000-0000-000000000001';"

# ------------------------------------------------------------
# CONFIRMA ESTADO FINAL
# ------------------------------------------------------------

$published = docker exec jungle-postgres `
    psql `
    -U jungle `
    -d jungle `
    -t `
    -A `
    -c "SELECT published_at IS NOT NULL
        FROM outbox_events
        WHERE id =
            '60000000-0000-0000-0000-000000000001';"

$published = $published.Trim()

if ($published -ne "t") {
    Write-Host "FALHOU: evento nao ficou marcado como publicado."
    exit 1
}

Write-Host "OK - evento ficou persistido como publicado."
Write-Host ""

Write-Host "========================================"
Write-Host " TODOS OS TESTES PASSARAM"
Write-Host "========================================"