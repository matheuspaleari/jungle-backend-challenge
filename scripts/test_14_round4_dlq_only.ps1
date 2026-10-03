$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root
$report = Join-Path $root "dlq_14_round4_validation.txt"

"#14 ROUND 4 - DLQ ONLY" | Set-Content $report
"Started: $(Get-Date -Format o)" | Add-Content $report

function Pass([string]$m) { "[PASS] $m" | Tee-Object -FilePath $report -Append | Write-Host }
function Fail([string]$m) { "[FAIL] $m" | Tee-Object -FilePath $report -Append | Write-Host; exit 1 }
function QueueURL([string]$name) {
    $v = docker exec jungle-localstack awslocal sqs get-queue-url --region us-east-1 --queue-name $name --query QueueUrl --output text
    if ($LASTEXITCODE -ne 0) { Fail "queue lookup failed: $name" }
    return (($v | Out-String).Trim())
}
function SendFIFO([string]$queue,[string]$body,[string]$group,[string]$dedup) {
    $tmp = Join-Path $env:TEMP ("jungle-dlq-" + [guid]::NewGuid().ToString() + ".json")
    [IO.File]::WriteAllText($tmp,$body,(New-Object Text.UTF8Encoding($false)))
    $cp = "/tmp/" + [IO.Path]::GetFileName($tmp)
    docker cp $tmp "jungle-localstack:$cp" *> $null
    Remove-Item $tmp -Force
    docker exec jungle-localstack awslocal sqs send-message --region us-east-1 --queue-url $queue --message-body "file://$cp" --message-group-id $group --message-deduplication-id $dedup *> $null
    $code=$LASTEXITCODE
    docker exec jungle-localstack rm -f $cp *> $null
    if ($code -ne 0) { Fail "send-message failed" }
}

docker inspect jungle-localstack *> $null
if ($LASTEXITCODE -ne 0) { Fail "LocalStack indisponivel" }
Pass "LocalStack disponivel"

$queue = QueueURL "wager-transactions.fifo"
$dlq = QueueURL "wager-transactions-dlq.fifo"

Write-Host ""
Write-Host "=== REDRIVE CONFIG ==="
$attrs = docker exec jungle-localstack awslocal sqs get-queue-attributes --region us-east-1 --queue-url $queue --attribute-names RedrivePolicy VisibilityTimeout --output json
$attrs | Tee-Object -FilePath $report -Append | Write-Host

# The consumer explicitly requests VisibilityTimeout=30 on ReceiveMessage.
# maxReceiveCount=5 therefore needs roughly 5 * 30s before LocalStack can redrive.
# Use a 240s observation window to avoid the previous false negative.
$suffix=[DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
$marker="round4-dlq-marker-$suffix"
$body="{not-valid-json-$marker"
SendFIFO $queue $body "round4-dlq" $marker
Pass "poison message unica enviada: $marker"

Write-Host ""
Write-Host "=== WAITING FOR REAL DLQ REDRIVE (max 240s) ==="
$found=$false
$deadline=(Get-Date).AddSeconds(240)

while((Get-Date) -lt $deadline -and -not $found){
    Start-Sleep -Seconds 5

    $srcAttrs = docker exec jungle-localstack awslocal sqs get-queue-attributes --region us-east-1 --queue-url $queue --attribute-names ApproximateNumberOfMessages ApproximateNumberOfMessagesNotVisible --output json
    $dlqAttrs = docker exec jungle-localstack awslocal sqs get-queue-attributes --region us-east-1 --queue-url $dlq --attribute-names ApproximateNumberOfMessages ApproximateNumberOfMessagesNotVisible --output json
    $stamp=Get-Date -Format "HH:mm:ss"
    "[$stamp] source=$srcAttrs dlq=$dlqAttrs" | Add-Content $report

    $raw = docker exec jungle-localstack awslocal sqs receive-message --region us-east-1 --queue-url $dlq --max-number-of-messages 10 --visibility-timeout 1 --wait-time-seconds 0 --output json
    if($LASTEXITCODE -eq 0 -and $raw){
        try {
            $obj=$raw | ConvertFrom-Json
            foreach($m in @($obj.Messages)){
                if($m.Body -and $m.Body.Contains($marker)){
                    $found=$true
                    break
                }
            }
        } catch {}
    }
}

if(-not $found){ Fail "poison message especifica nao observada na DLQ em 240s: $marker" }

Pass "poison message especifica chegou realmente a DLQ"
"`r`n14_ROUND4_DLQ_RESULT=PASS" | Add-Content $report
Write-Host ""
Write-Host "#14 DLQ FINAL PASS"
Write-Host "Report: $report"
