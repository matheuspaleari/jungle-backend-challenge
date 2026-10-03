$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root
$report = Join-Path $root "final_acceptance_14_round4.txt"

"#14 ROUND 4 - FINAL ACCEPTANCE (NON-DESTRUCTIVE)" | Set-Content $report
"Started: $(Get-Date -Format o)" | Add-Content $report

$script:failures = 0
function Pass([string]$m) { "[PASS] $m" | Tee-Object -FilePath $report -Append | Write-Host }
function Fail([string]$m) { $script:failures++; "[FAIL] $m" | Tee-Object -FilePath $report -Append | Write-Host }
function Section([string]$m) { "`r`n=== $m ===" | Tee-Object -FilePath $report -Append | Write-Host }
function Eq($actual,$expected,[string]$label) {
    if ("$actual" -eq "$expected") { Pass "$label | $actual" } else { Fail "$label | esperado=$expected obtido=$actual" }
}
function PsqlScalar([string]$sql) {
    $v = docker exec jungle-postgres psql -U jungle -d jungle -At -c $sql
    if ($LASTEXITCODE -ne 0) { throw "psql failed" }
    return (($v | Out-String).Trim())
}
function QueueURL([string]$name) {
    $v = docker exec jungle-localstack awslocal sqs get-queue-url --region us-east-1 --queue-name $name --query QueueUrl --output text
    if ($LASTEXITCODE -ne 0) { throw "queue lookup failed: $name" }
    return (($v | Out-String).Trim())
}
function SendFIFO([string]$queue,[string]$body,[string]$group,[string]$dedup) {
    $tmp = Join-Path $env:TEMP ("jungle-sqs-" + [guid]::NewGuid().ToString() + ".json")
    [IO.File]::WriteAllText($tmp,$body,(New-Object Text.UTF8Encoding($false)))
    $containerPath = "/tmp/" + [IO.Path]::GetFileName($tmp)
    docker cp $tmp "jungle-localstack:$containerPath" *> $null
    Remove-Item $tmp -Force
    docker exec jungle-localstack awslocal sqs send-message --region us-east-1 --queue-url $queue --message-body "file://$containerPath" --message-group-id $group --message-deduplication-id $dedup *> $null
    $code=$LASTEXITCODE
    docker exec jungle-localstack rm -f $containerPath *> $null
    if ($code -ne 0) { throw "send-message failed" }
}

Section "0. PRECONDITIONS"
docker inspect jungle-postgres *> $null
if ($LASTEXITCODE -eq 0) { Pass "PostgreSQL container disponivel" } else { Fail "PostgreSQL container ausente" }
docker inspect jungle-localstack *> $null
if ($LASTEXITCODE -eq 0) { Pass "LocalStack container disponivel" } else { Fail "LocalStack container ausente" }
docker inspect jungle-keycloak *> $null
if ($LASTEXITCODE -eq 0) { Pass "Keycloak container disponivel" } else { Fail "Keycloak container ausente" }

try {
    $live = Invoke-RestMethod -Uri "http://localhost:8080/health/live" -TimeoutSec 5
    Pass "API ativa em :8080"
} catch {
    Fail "API nao esta ativa em http://localhost:8080"
    Write-Host "Inicie a API com o mesmo ambiente usado no #10/#12 e rode este script novamente."
    exit 1
}

try {
    $token = (Invoke-RestMethod -Method POST `
        -Uri "http://localhost:8081/realms/jungle/protocol/openid-connect/token" `
        -ContentType "application/x-www-form-urlencoded" `
        -Body @{grant_type="client_credentials";client_id="provider-a";client_secret="provider-a-secret"}).access_token
    if ($token) { Pass "token provider-a obtido" } else { Fail "token provider-a vazio" }
} catch { Fail "nao foi possivel obter token provider-a"; exit 1 }

$headers = @{ Authorization="Bearer $token" }
$suffix = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()

Section "1. OPENING POSITIVO - EXACT CONTRACT"
$playerPos = "round4-opening-pos-$suffix"
$bodyPos = @{playerId=$playerPos;initialBalance=@{amount="100.00";currency="BRL"}} | ConvertTo-Json -Depth 5
$walletPos = Invoke-RestMethod -Method POST -Uri "http://localhost:8080/wallets" -Headers $headers -ContentType "application/json" -Body $bodyPos
$widPos = $walletPos.id
Eq $walletPos.balance.amount "100.00" "saldo inicial positivo"
Eq $walletPos.version 1 "wallet version opening"

$openingCount = PsqlScalar "SELECT count(*) FROM wager_transactions WHERE wallet_id='$widPos' AND kind='OPENING' AND state='PROCESSED';"
$ledgerCount = PsqlScalar "SELECT count(*) FROM ledger_entries WHERE wallet_id='$widPos' AND direction='CREDIT' AND value_minor=10000;"
$processedCount = PsqlScalar "SELECT count(*) FROM outbox_events o JOIN wager_transactions w ON w.id::text=o.aggregate_id WHERE w.wallet_id='$widPos' AND w.kind='OPENING' AND o.event_type='WagerTransactionProcessed';"
$changedCount = PsqlScalar "SELECT count(*) FROM outbox_events WHERE aggregate_id='$widPos' AND event_type='WalletBalanceChanged';"
Eq $openingCount 1 "OPENING PROCESSED exato"
Eq $ledgerCount 1 "OPENING CREDIT ledger exato"
Eq $processedCount 1 "OPENING WagerTransactionProcessed exato"
Eq $changedCount 1 "OPENING WalletBalanceChanged exato"

Section "2. OPENING ZERO - NO FINANCIAL ARTIFACTS"
$playerZero = "round4-opening-zero-$suffix"
$bodyZero = @{playerId=$playerZero;initialBalance=@{amount="0.00";currency="BRL"}} | ConvertTo-Json -Depth 5
$walletZero = Invoke-RestMethod -Method POST -Uri "http://localhost:8080/wallets" -Headers $headers -ContentType "application/json" -Body $bodyZero
$widZero = $walletZero.id
Eq $walletZero.balance.amount "0.00" "saldo inicial zero"
Eq $walletZero.version 1 "wallet zero version"
Eq (PsqlScalar "SELECT count(*) FROM wager_transactions WHERE wallet_id='$widZero' AND kind='OPENING';") 0 "zero nao cria OPENING"
Eq (PsqlScalar "SELECT count(*) FROM ledger_entries WHERE wallet_id='$widZero';") 0 "zero nao cria ledger"
Eq (PsqlScalar "SELECT count(*) FROM outbox_events WHERE aggregate_id='$widZero';") 0 "zero nao cria evento financeiro"

Section "3. HTTP -> SQS CROSS-TRANSPORT REPLAY"
$playerCross = "round4-cross-$suffix"
$walletCross = Invoke-RestMethod -Method POST -Uri "http://localhost:8080/wallets" -Headers $headers -ContentType "application/json" -Body (@{playerId=$playerCross;initialBalance=@{amount="100.00";currency="BRL"}} | ConvertTo-Json -Depth 5)
$widCross = $walletCross.id
$ext = "round4-cross-tx-$suffix"
$idem = "provider-a:$ext"
$round = "round4-cross-round-$suffix"
$httpBody = @{
    providerId="provider-a"; externalTransactionId=$ext; playerId=$playerCross; walletId=$widCross
    roundId=$round; gameId="fortune-chimp"; kind="BET"; money=@{amount="25.00";currency="BRL"}
} | ConvertTo-Json -Depth 6
$httpHeaders = @{Authorization="Bearer $token";"Idempotency-Key"=$idem}
$first = Invoke-RestMethod -Method POST -Uri "http://localhost:8080/wagering/transactions" -Headers $httpHeaders -ContentType "application/json" -Body $httpBody
Eq $first.status "PROCESSED" "HTTP original processado"
Eq $first.idempotentReplay $false "HTTP original nao replay"
Eq $first.balance.amount "75.00" "HTTP saldo apos BET"

$queue = QueueURL "wager-transactions.fifo"
$msgId = "round4-msg-$suffix"
$envelope = @{
    messageId=$msgId
    type="WagerTransactionRequested"
    occurredAt=[DateTime]::UtcNow.ToString("yyyy-MM-ddTHH:mm:ss.fffZ")
    data=@{
        providerId="provider-a"; externalTransactionId=$ext; idempotencyKey=$idem
        playerId=$playerCross; walletId=$widCross; roundId=$round; gameId="fortune-chimp"
        kind="BET"; money=@{amount="25.00";currency="BRL"}
    }
} | ConvertTo-Json -Depth 8 -Compress
SendFIFO $queue $envelope "round4-cross" ("round4-cross-" + $suffix)

$completed=$false
for($i=0;$i -lt 30;$i++){
    Start-Sleep -Milliseconds 500
    $inbox=PsqlScalar "SELECT count(*) FROM inbox_messages WHERE message_id='$msgId' AND completed_at IS NOT NULL;"
    if($inbox -eq "1"){ $completed=$true; break }
}
if($completed){Pass "SQS cross-transport consumido e inbox concluida"}else{Fail "SQS cross-transport nao concluiu inbox"}

$balanceCross=PsqlScalar "SELECT balance_minor FROM wallets WHERE id='$widCross';"
$versionCross=PsqlScalar "SELECT version FROM wallets WHERE id='$widCross';"
$betTxCount=PsqlScalar "SELECT count(*) FROM wager_transactions WHERE provider_id='provider-a' AND external_transaction_id='$ext' AND state='PROCESSED';"
$debitCount=PsqlScalar "SELECT count(*) FROM ledger_entries l JOIN wager_transactions w ON w.id=l.transaction_id WHERE w.provider_id='provider-a' AND w.external_transaction_id='$ext' AND l.direction='DEBIT';"
Eq $balanceCross 7500 "cross-transport nao duplica saldo"
Eq $versionCross 2 "cross-transport nao incrementa version novamente"
Eq $betTxCount 1 "cross-transport mantem uma transacao"
Eq $debitCount 1 "cross-transport mantem um unico DEBIT"

Section "4. REAL DLQ REDRIVE"
$dlq = QueueURL "wager-transactions-dlq.fifo"
# Keep the test fast. The consumer receives with its configured visibility timeout,
# so change the queue default only for this test and restore it afterwards.
docker exec jungle-localstack awslocal sqs set-queue-attributes --region us-east-1 --queue-url $queue --attributes VisibilityTimeout=1 *> $null

$poisonDedup="round4-poison-$suffix"
SendFIFO $queue "{not-valid-json" "round4-poison" $poisonDedup
Pass "poison message enviada"

$found=$false
$dlqBody=""
for($i=0;$i -lt 90;$i++){
    Start-Sleep -Seconds 1
    $raw = docker exec jungle-localstack awslocal sqs receive-message --region us-east-1 --queue-url $dlq --max-number-of-messages 10 --visibility-timeout 1 --wait-time-seconds 0 --output json
    if($LASTEXITCODE -eq 0 -and $raw){
        try {
            $obj=$raw | ConvertFrom-Json
            if($obj.Messages -and $obj.Messages.Count -gt 0){
                foreach($m in $obj.Messages){
                    if($m.Body -eq "{not-valid-json"){ $found=$true; $dlqBody=$m.Body; break }
                }
            }
        } catch {}
    }
    if($found){break}
}
docker exec jungle-localstack awslocal sqs set-queue-attributes --region us-east-1 --queue-url $queue --attributes VisibilityTimeout=30 *> $null

if($found){Pass "poison message chegou realmente a DLQ"}else{Fail "poison message nao observada na DLQ em 90s"}

Section "5. FINAL CONSISTENCY"
$recon = Invoke-RestMethod -Method POST -Uri "http://localhost:8080/wallets/$widCross/reconciliation" -Headers $headers
Eq $recon.consistent $true "reconciliation cross-transport consistente"
Eq $recon.difference.amount "0.00" "reconciliation difference zero"
Eq $recon.checkedEntries 2 "ledger OPENING + BET"

"`r`nFailures=$script:failures" | Add-Content $report
if($script:failures -eq 0){
    "`r`n14_ROUND4_RESULT=PASS" | Add-Content $report
    Write-Host ""
    Write-Host "#14 round 4 FINAL ACCEPTANCE PASS"
    Write-Host "Report: $report"
    exit 0
}
"`r`n14_ROUND4_RESULT=FAIL" | Add-Content $report
Write-Host ""
Write-Host "#14 round 4 FAIL - failures=$script:failures"
Write-Host "Report: $report"
exit 1
