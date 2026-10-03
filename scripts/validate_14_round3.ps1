$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$report = Join-Path $root "acceptance_14_round3.txt"
"#14 ROUND 3 - ACCEPTANCE / REQUIREMENT CLOSURE" | Set-Content $report
"Started: $(Get-Date -Format o)" | Add-Content $report

if (-not $env:TEST_DATABASE_URL) {
    $env:TEST_DATABASE_URL = "postgres://jungle:jungle@localhost:5432/jungle?sslmode=disable"
}

$script:failures = 0
$script:reviews = 0

function Pass([string]$m) {
    "[PASS] $m" | Tee-Object -FilePath $report -Append | Write-Host
}
function Fail([string]$m) {
    $script:failures++
    "[FAIL] $m" | Tee-Object -FilePath $report -Append | Write-Host
}
function Review([string]$m) {
    $script:reviews++
    "[REVIEW] $m" | Tee-Object -FilePath $report -Append | Write-Host
}
function Section([string]$m) {
    "`r`n=== $m ===" | Tee-Object -FilePath $report -Append | Write-Host
}
function RunGoTest([string]$Package, [string]$Pattern, [string]$Label) {
    $old = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    $output = & go test $Package -run $Pattern -count=1 -v 2>&1
    $code = $LASTEXITCODE
    $ErrorActionPreference = $old
    $output | ForEach-Object { "$_" } | Add-Content $report
    if ($code -eq 0) { Pass $Label } else { Fail $Label }
}
function SourceContains([string]$Path, [string[]]$Patterns, [string]$Label) {
    if (-not (Test-Path $Path)) {
        Fail "$Label - arquivo ausente: $Path"
        return
    }
    $text = Get-Content $Path -Raw
    foreach ($p in $Patterns) {
        if ($text -notmatch $p) {
            Fail "$Label - padrao ausente: $p"
            return
        }
    }
    Pass $Label
}

Section "1. BASELINE"
$old = $ErrorActionPreference
$ErrorActionPreference = "Continue"
$out = & go test ./... -count=1 2>&1
$code = $LASTEXITCODE
$ErrorActionPreference = $old
$out | ForEach-Object { "$_" } | Add-Content $report
if ($code -eq 0) { Pass "go test ./..." } else { Fail "go test ./..." }

Section "2. MONEY / LOSS"
RunGoTest "./internal/domain" "Test(NewMoney|ZeroMoney|Money|Loss|BetRejectsZero)" "money sem float + LOSS zero + BET positivo"

Section "3. BET / WIN / LOSS / ATOMICIDADE"
RunGoTest "./internal/application" "TestWagerProcessor(BetAtomic|WinAtomic|LossDoesNotMoveBalance|InsufficientFundsIsDurableRejection)Integration" "operacoes financeiras basicas e rejeicao duravel"

Section "4. IDEMPOTENCIA"
RunGoTest "./internal/application" "TestWagerProcessor(ConcurrentIdenticalRequestsAreIdempotent|ExternalTransactionConflictIsStable|RejectedResultReplaysDeterministically|PendingReferenceReplaysDeterministically|IdempotencyIsIsolatedByProvider)Integration" "idempotencia persistente, conflito e replay"
RunGoTest "./internal/application" "TestWagerPayloadHashNormalizesTransportEquivalentPayloads" "hash canonico compartilhado por payload equivalente"

Section "5. REVERSOES"
RunGoTest "./internal/application" "TestWagerProcessor(RefundCreditsProcessedBet|RollbackWinDebits|RejectsSecondSuccessfulReversal)Integration" "REFUND/ROLLBACK e segunda reversao"
RunGoTest "./internal/application" "Test(RollbackInsufficientFundsUsesDistinctFailureCode|ConcurrentRefundAndRollbackAllowOnlyOneSuccessfulReversal)Integration" "rollback sem saldo + REFUND/ROLLBACK concorrentes"

Section "6. PENDING REFERENCE"
RunGoTest "./internal/application" "TestPendingReferenceWorker" "retry, expiracao, max attempts e concorrencia do pending reference"

Section "7. CONCORRENCIA"
RunGoTest "./internal/application" "TestWagerProcessor(ThreeIndependentClientsPreserveWalletInvariant|DifferentWalletsDoNotShareDatabaseLock)Integration" "3 clientes independentes + wallets independentes"

Section "8. OUTBOX"
RunGoTest "./internal/infrastructure/outboxpublisher" "Test(PublisherPreservesEventIDWhenConfirmationFailsAndEventIsRepublished|PublisherSchedulesBackoffWhenSQSPublishFails)" "outbox retry + publish/confirm recovery"
RunGoTest "./internal/infrastructure/postgres" "TestOutboxClaimIsExclusiveAcrossPublishersAndExpiredLeaseRecovers" "claim exclusivo + lease recovery"

Section "9. INBOX"
RunGoTest "./internal/infrastructure/postgres" "TestInboxReceiveAndCompleteRollbackTogetherIntegration" "inbox receive/complete rollback atomico"

Section "10. AUTH / PROVIDER ISOLATION"
RunGoTest "./internal/transport/httpapi" "TestAuthenticationMiddleware" "middleware OIDC fail-closed"
RunGoTest "./internal/transport/httpapi" "TestRequireWalletOwner" "provider ownership"
RunGoTest "./internal/infrastructure/postgres" "TestWalletRepositoryAllowsSamePlayerAndCurrencyAcrossProvidersIntegration" "wallet uniqueness provider-scoped"

Section "11. OPENING CONTRACT - STRUCTURAL"
SourceContains ".\internal\application\wallet_service.go" @(
    "initialBalance\.IsZero\(\)",
    "WagerKindOpening",
    "WagerStatusProcessed",
    "LedgerDirectionCredit",
    "EventTypeWagerTransactionProcessed",
    "EventTypeWalletBalanceChanged"
) "OPENING positivo cria transaction/ledger/eventos e zero faz early return"

Section "12. TRANSPORT HASH EQUIVALENCE - STRUCTURAL"
SourceContains ".\internal\transport\httpapi\canonical_hash.go" @(
    "application\.WagerPayloadHash|sha256"
) "HTTP possui canonical hash"
SourceContains ".\internal\infrastructure\sqsconsumer\consumer.go" @(
    "WagerPayloadHash",
    "BusinessPayload"
) "SQS usa hash de business payload"

Section "13. RACE EVIDENCE"
if (Test-Path ".\race_14_validation.txt") {
    $race = Get-Content ".\race_14_validation.txt" -Raw
    if ($race -match "14_RACE_RESULT=PASS") {
        Pass "go test -race ./... em Linux"
    } else {
        Review "race_14_validation.txt existe, mas nao contem 14_RACE_RESULT=PASS"
    }
} else {
    Review "race_14_validation.txt nao encontrado"
}

Section "14. DELIVERY CLEANLINESS"
if (Test-Path ".\.gitignore") {
    $gi = Get-Content ".\.gitignore" -Raw
    if ($gi -match "\.env" -and $gi -match "\.tmp-" -and $gi -match "\*\.exe") {
        Pass ".gitignore cobre segredo e artefatos principais"
    } else {
        Review ".gitignore precisa revisao"
    }
} else {
    Fail ".gitignore ausente"
}

$trackedEnv = git ls-files ".env" 2>$null
if ([string]::IsNullOrWhiteSpace($trackedEnv)) {
    Pass ".env nao esta rastreado pelo Git"
} else {
    Fail ".env esta rastreado pelo Git"
}

$trackedExe = git ls-files "*.exe" 2>$null
if ([string]::IsNullOrWhiteSpace($trackedExe)) {
    Pass "nenhum .exe rastreado"
} else {
    Fail "ha .exe rastreado"
}

Section "15. REMAINING ACCEPTANCE GAPS"
Review "OPENING: implementacao estrutural validada; ainda queremos prova de integracao especifica para positivo/zero + contagem exata de ledger/outbox."
Review "HTTP <-> SQS: hash compartilhado/normalizado validado; ainda queremos replay cross-transport E2E da mesma operacao."
Review "DLQ: ainda queremos evidenciar chegada real na DLQ, nao apenas contador por receive count."
Review "CLEAN START: ainda queremos subir o projeto a partir de estado limpo e executar smoke final."

"`r`nFailures=$script:failures Reviews=$script:reviews" | Add-Content $report

if ($script:failures -gt 0) {
    "`r`n14_ROUND3_RESULT=FAIL" | Add-Content $report
    Write-Host ""
    Write-Host "#14 round 3 FAIL - failures=$script:failures reviews=$script:reviews"
    Write-Host "Report: $report"
    exit 1
}

"`r`n14_ROUND3_RESULT=PASS_WITH_REVIEW" | Add-Content $report
Write-Host ""
Write-Host "#14 round 3 PASS_WITH_REVIEW"
Write-Host "Reviews esperados nesta rodada: $script:reviews"
Write-Host "Report: $report"
