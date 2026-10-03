$ErrorActionPreference = "Stop"

Write-Host ""
Write-Host "========================================"
Write-Host " Inbox Database Constraints Test"
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

Run-Sql "DELETE FROM inbox_messages
        WHERE consumer_name LIKE 'inbox-test-%';"

Write-Host "OK - dados preparados."
Write-Host ""

# ------------------------------------------------------------
# MENSAGEM PENDENTE VALIDA
# ------------------------------------------------------------

Expect-Success `
    "Mensagem pendente valida" `
    "INSERT INTO inbox_messages (
        id,
        consumer_name,
        message_id,
        payload_hash,
        received_at
    )
    VALUES (
        '50000000-0000-0000-0000-000000000001',
        'inbox-test-wager-consumer',
        'message-001',
        'hash-001',
        NOW()
    );"

# ------------------------------------------------------------
# MESMO CONSUMER + MESSAGE ID
#
# Simula redelivery da mesma mensagem.
# O banco deve impedir outro registro.
# ------------------------------------------------------------

Expect-Failure `
    "Redelivery duplicada para o mesmo consumer" `
    "INSERT INTO inbox_messages (
        id,
        consumer_name,
        message_id,
        payload_hash,
        received_at
    )
    VALUES (
        '50000000-0000-0000-0000-000000000002',
        'inbox-test-wager-consumer',
        'message-001',
        'hash-001',
        NOW()
    );"

# ------------------------------------------------------------
# MESMO MESSAGE ID, OUTRO CONSUMER
#
# Deve ser permitido porque a identidade do Inbox
# e composta por consumer_name + message_id.
# ------------------------------------------------------------

Expect-Success `
    "Mesmo messageId para outro consumer" `
    "INSERT INTO inbox_messages (
        id,
        consumer_name,
        message_id,
        payload_hash,
        received_at
    )
    VALUES (
        '50000000-0000-0000-0000-000000000003',
        'inbox-test-audit-consumer',
        'message-001',
        'hash-001',
        NOW()
    );"

# ------------------------------------------------------------
# CONSUMER VAZIO
# ------------------------------------------------------------

Expect-Failure `
    "Consumer vazio" `
    "INSERT INTO inbox_messages (
        id,
        consumer_name,
        message_id,
        payload_hash,
        received_at
    )
    VALUES (
        '50000000-0000-0000-0000-000000000004',
        '',
        'message-004',
        'hash-004',
        NOW()
    );"

# ------------------------------------------------------------
# MESSAGE ID VAZIO
# ------------------------------------------------------------

Expect-Failure `
    "MessageId vazio" `
    "INSERT INTO inbox_messages (
        id,
        consumer_name,
        message_id,
        payload_hash,
        received_at
    )
    VALUES (
        '50000000-0000-0000-0000-000000000005',
        'inbox-test-wager-consumer',
        '',
        'hash-005',
        NOW()
    );"

# ------------------------------------------------------------
# PAYLOAD HASH VAZIO
# ------------------------------------------------------------

Expect-Failure `
    "Payload hash vazio" `
    "INSERT INTO inbox_messages (
        id,
        consumer_name,
        message_id,
        payload_hash,
        received_at
    )
    VALUES (
        '50000000-0000-0000-0000-000000000006',
        'inbox-test-wager-consumer',
        'message-006',
        '',
        NOW()
    );"

# ------------------------------------------------------------
# COMPLETED_AT ANTERIOR AO RECEIVED_AT
# ------------------------------------------------------------

Expect-Failure `
    "Conclusao anterior ao recebimento" `
    "INSERT INTO inbox_messages (
        id,
        consumer_name,
        message_id,
        payload_hash,
        received_at,
        completed_at
    )
    VALUES (
        '50000000-0000-0000-0000-000000000007',
        'inbox-test-wager-consumer',
        'message-007',
        'hash-007',
        '2026-01-02 10:00:00+00',
        '2026-01-02 09:00:00+00'
    );"

# ------------------------------------------------------------
# CONCLUIR MENSAGEM
# ------------------------------------------------------------

Expect-Success `
    "Concluir mensagem recebida" `
    "UPDATE inbox_messages
     SET completed_at = NOW()
     WHERE consumer_name = 'inbox-test-wager-consumer'
       AND message_id = 'message-001';"

# ------------------------------------------------------------
# CONFIRMA ESTADO FINAL
# ------------------------------------------------------------

$completed = docker exec jungle-postgres `
    psql `
    -U jungle `
    -d jungle `
    -t `
    -A `
    -c "SELECT completed_at IS NOT NULL
        FROM inbox_messages
        WHERE consumer_name = 'inbox-test-wager-consumer'
          AND message_id = 'message-001';"

$completed = $completed.Trim()

if ($completed -ne "t") {
    Write-Host "FALHOU: mensagem nao ficou marcada como concluida."
    exit 1
}

Write-Host "OK - mensagem ficou persistida como concluida."
Write-Host ""

# ------------------------------------------------------------
# GARANTE QUE DUPLICIDADE NAO CRIOU OUTRA LINHA
# ------------------------------------------------------------

$count = docker exec jungle-postgres `
    psql `
    -U jungle `
    -d jungle `
    -t `
    -A `
    -c "SELECT COUNT(*)
        FROM inbox_messages
        WHERE consumer_name = 'inbox-test-wager-consumer'
          AND message_id = 'message-001';"

$count = $count.Trim()

if ($count -ne "1") {
    Write-Host "FALHOU: redelivery criou mais de um registro."
    exit 1
}

Write-Host "OK - redelivery nao criou registro duplicado."
Write-Host ""

Write-Host "========================================"
Write-Host " TODOS OS TESTES PASSARAM"
Write-Host "========================================"