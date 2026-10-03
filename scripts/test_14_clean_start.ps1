$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root
$report = Join-Path $root "clean_start_14_validation.txt"
"#14 CLEAN START - DESTRUCTIVE FINAL TEST" | Set-Content $report
"Started: $(Get-Date -Format o)" | Add-Content $report

$fail=0
function Pass($m){ "[PASS] $m" | Tee-Object -FilePath $report -Append | Write-Host }
function Fail($m){ $script:fail++; "[FAIL] $m" | Tee-Object -FilePath $report -Append | Write-Host }
function Section($m){ "`r`n=== $m ===" | Tee-Object -FilePath $report -Append | Write-Host }
function Wait-Until([scriptblock]$Test,[int]$Seconds,[string]$Label){
  $deadline=(Get-Date).AddSeconds($Seconds)
  while((Get-Date) -lt $deadline){
    try { if(& $Test){ Pass $Label; return $true } } catch {}
    Start-Sleep -Seconds 2
  }
  Fail "$Label (timeout ${Seconds}s)"; return $false
}

Section "0. DESTRUCTIVE RESET"
Write-Host "ATENCAO: removendo containers e volume PostgreSQL do desafio."
docker rm -f jungle-keycloak 2>$null | Out-Null
$old=$ErrorActionPreference; $ErrorActionPreference="Continue"
docker compose down -v --remove-orphans 2>&1 | Tee-Object -FilePath $report -Append | Write-Host
$dc=$LASTEXITCODE
$ErrorActionPreference=$old
if($dc -eq 0){Pass "docker compose down -v"}else{Fail "docker compose down -v"; exit 1}

Section "1. FRESH INFRA"
# Docker Compose writes normal progress lines to stderr. In Windows PowerShell,
# ErrorActionPreference=Stop can incorrectly promote those lines to NativeCommandError.
$old=$ErrorActionPreference
$ErrorActionPreference="Continue"
$composeOutput = & docker compose up -d postgres localstack 2>&1
$composeCode = $LASTEXITCODE
$ErrorActionPreference=$old
$composeOutput | ForEach-Object { "$_" } | Tee-Object -FilePath $report -Append | Write-Host
if($composeCode -ne 0){Fail "docker compose up (exit code $composeCode)"; exit 1}
Pass "docker compose up -d postgres localstack"
Wait-Until { (docker inspect -f '{{.State.Health.Status}}' jungle-postgres 2>$null) -eq "healthy" } 90 "PostgreSQL fresh healthy" | Out-Null
Wait-Until { (docker inspect -f '{{.State.Health.Status}}' jungle-localstack 2>$null) -eq "healthy" } 120 "LocalStack fresh healthy" | Out-Null

Section "2. FRESH KEYCLOAK"
$realm=(Resolve-Path ".\keycloak\realm-export.json").Path
docker run -d --name jungle-keycloak -p 8081:8080 `
  -e KC_BOOTSTRAP_ADMIN_USERNAME=admin `
  -e KC_BOOTSTRAP_ADMIN_PASSWORD=admin `
  --mount "type=bind,source=$realm,target=/opt/keycloak/data/import/realm-export.json,readonly" `
  quay.io/keycloak/keycloak:26.8.0 start-dev --import-realm 2>&1 |
  Tee-Object -FilePath $report -Append | Write-Host
if($LASTEXITCODE -ne 0){Fail "Keycloak fresh start"; exit 1}
Wait-Until {
  try { (Invoke-WebRequest -UseBasicParsing "http://localhost:8081/realms/jungle/.well-known/openid-configuration" -TimeoutSec 3).StatusCode -eq 200 } catch {$false}
} 150 "Keycloak realm importado" | Out-Null

Section "3. QUEUES FROM INIT"
$queues = docker exec jungle-localstack awslocal sqs list-queues --region us-east-1 --output json
$queues | Tee-Object -FilePath $report -Append | Write-Host
if("$queues" -match "wager-transactions.fifo"){Pass "input FIFO criada automaticamente"}else{Fail "input FIFO ausente"}
if("$queues" -match "wager-transactions-dlq.fifo"){Pass "DLQ criada automaticamente"}else{Fail "DLQ ausente"}

Section "4. MIGRATIONS 000001 -> 000011"
$env:DATABASE_URL="postgres://jungle:jungle@localhost:5432/jungle?sslmode=disable"
$old=$ErrorActionPreference; $ErrorActionPreference="Continue"
$mout=& powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "& '.\scripts\migrate-up.ps1' 2>&1; exit `$LASTEXITCODE"
$mc=$LASTEXITCODE
$ErrorActionPreference=$old
$mout | Tee-Object -FilePath $report -Append | Write-Host
if($mc -eq 0){Pass "migrations fresh database"}else{Fail "migrations fresh database"; exit 1}

$versions=docker exec jungle-postgres psql -U jungle -d jungle -At -c "SELECT version FROM schema_migrations ORDER BY version;"
$versions | Tee-Object -FilePath $report -Append | Write-Host
$count=docker exec jungle-postgres psql -U jungle -d jungle -At -c "SELECT count(*) FROM schema_migrations;"
if([int]$count -ge 11){Pass "11+ migrations registradas"}else{Fail "esperadas >=11 migrations, obtido $count"}
$legacy=docker exec jungle-postgres psql -U jungle -d jungle -At -c "SELECT EXISTS(SELECT 1 FROM pg_constraint WHERE conname='wallets_player_currency_unique');"
if($legacy.Trim() -eq "f"){Pass "constraint global removida no fresh DB"}else{Fail "constraint global ainda presente"}
$scoped=docker exec jungle-postgres psql -U jungle -d jungle -At -c "SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE indexname='wallets_provider_player_currency_unique');"
if($scoped.Trim() -eq "t"){Pass "provider-scoped uniqueness presente"}else{Fail "provider-scoped uniqueness ausente"}

Section "5. BUILD + START APPLICATION"
go build -o .\.tmp-clean-api.exe .\cmd\api
if($LASTEXITCODE -eq 0){Pass "go build fresh"}else{Fail "go build"; exit 1}

$apiOut=Join-Path $root ".tmp-clean-api.out.log"
$apiErr=Join-Path $root ".tmp-clean-api.err.log"
Remove-Item $apiOut,$apiErr -Force -ErrorAction SilentlyContinue
$cmd=@"
`$env:DATABASE_URL='postgres://jungle:jungle@localhost:5432/jungle?sslmode=disable'
`$env:SQS_ENDPOINT='http://localhost:4566'
`$env:AWS_REGION='us-east-1'
`$env:SQS_QUEUE_URL='http://localhost:4566/000000000000/wager-transactions.fifo'
& '$root\.tmp-clean-api.exe'
"@
$api=Start-Process powershell.exe -ArgumentList "-NoProfile","-Command",$cmd -PassThru -RedirectStandardOutput $apiOut -RedirectStandardError $apiErr
Pass "API process started PID=$($api.Id)"

if(-not (Wait-Until {
  try { (Invoke-WebRequest -UseBasicParsing "http://localhost:8080/health/live" -TimeoutSec 3).StatusCode -eq 200 } catch {$false}
} 60 "health/live fresh")) {
  Get-Content $apiOut,$apiErr -ErrorAction SilentlyContinue | Add-Content $report
  exit 1
}
Wait-Until {
  try { (Invoke-WebRequest -UseBasicParsing "http://localhost:8080/health/ready" -TimeoutSec 3).StatusCode -eq 200 } catch {$false}
} 60 "health/ready fresh" | Out-Null

Section "6. AUTH + FINANCIAL SMOKE"
$token=$null
try {
  $token=(Invoke-RestMethod -Method POST -Uri "http://localhost:8081/realms/jungle/protocol/openid-connect/token" `
    -ContentType "application/x-www-form-urlencoded" `
    -Body @{grant_type="client_credentials";client_id="provider-a";client_secret="provider-a-secret"}).access_token
} catch {}
if($token){Pass "OIDC client_credentials fresh"}else{Fail "OIDC token"; exit 1}

try {
  Invoke-WebRequest -UseBasicParsing -Method GET -Uri "http://localhost:8080/wallets/not-a-wallet" -ErrorAction Stop | Out-Null
  Fail "business endpoint sem token deveria rejeitar"
} catch {
  if($_.Exception.Response.StatusCode.value__ -eq 401){Pass "business endpoint rejeita sem token"}else{Fail "sem token status inesperado"}
}

$h=@{Authorization="Bearer $token"}
$suffix=[DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
$player="clean-start-$suffix"
$w=Invoke-RestMethod -Method POST -Uri "http://localhost:8080/wallets" -Headers $h -ContentType "application/json" `
  -Body (@{playerId=$player;initialBalance=@{amount="100.00";currency="BRL"}}|ConvertTo-Json -Depth 5)
if($w.balance.amount -eq "100.00" -and $w.version -eq 1){Pass "wallet opening fresh 100.00/version1"}else{Fail "wallet opening fresh"}

$idem="provider-a:clean-bet-$suffix"
$wh=@{Authorization="Bearer $token";"Idempotency-Key"=$idem}
$bet=Invoke-RestMethod -Method POST -Uri "http://localhost:8080/wagering/transactions" -Headers $wh -ContentType "application/json" `
 -Body (@{providerId="provider-a";externalTransactionId="clean-bet-$suffix";playerId=$player;walletId=$w.id;roundId="clean-round-$suffix";gameId="fortune-chimp";kind="BET";money=@{amount="25.00";currency="BRL"}}|ConvertTo-Json -Depth 6)
if($bet.status -eq "PROCESSED" -and $bet.balance.amount -eq "75.00"){Pass "BET fresh -> 75.00"}else{Fail "BET fresh"}

$r=Invoke-RestMethod -Method POST -Uri "http://localhost:8080/wallets/$($w.id)/reconciliation" -Headers $h
if($r.consistent -eq $true -and $r.difference.amount -eq "0.00"){Pass "reconciliation fresh consistente"}else{Fail "reconciliation fresh"}

Section "7. DATABASE / OUTBOX EVIDENCE"
$ledger=docker exec jungle-postgres psql -U jungle -d jungle -At -c "SELECT count(*) FROM ledger_entries WHERE wallet_id='$($w.id)';"
if($ledger.Trim() -eq "2"){Pass "ledger fresh OPENING + BET"}else{Fail "ledger count esperado2 obtido $ledger"}
$outbox=docker exec jungle-postgres psql -U jungle -d jungle -At -c "SELECT count(*) FROM outbox_events;"
if([int]$outbox -ge 4){Pass "outbox events criados no fresh DB"}else{Fail "outbox insuficiente: $outbox"}

Section "8. TEST / VET"
$env:TEST_DATABASE_URL=$env:DATABASE_URL
go test ./... -count=1 2>&1 | Tee-Object -FilePath $report -Append | Write-Host
if($LASTEXITCODE -eq 0){Pass "go test ./... fresh"}else{Fail "go test ./... fresh"}
go vet ./... 2>&1 | Tee-Object -FilePath $report -Append | Write-Host
if($LASTEXITCODE -eq 0){Pass "go vet ./... fresh"}else{Fail "go vet ./... fresh"}

Section "9. CLEAN SHUTDOWN"
if($api -and -not $api.HasExited){
  Stop-Process -Id $api.Id -Force -ErrorAction SilentlyContinue
  Pass "API de smoke encerrada"
}
Remove-Item ".\.tmp-clean-api.exe" -Force -ErrorAction SilentlyContinue

"`r`nFailures=$fail" | Add-Content $report
if($fail -eq 0){
  "`r`n14_CLEAN_START_RESULT=PASS" | Add-Content $report
  Write-Host ""
  Write-Host "#14 CLEAN START PASS"
  Write-Host "Infra permanece ligada; API de smoke foi encerrada."
  Write-Host "Report: $report"
  exit 0
}
"`r`n14_CLEAN_START_RESULT=FAIL" | Add-Content $report
Write-Host ""
Write-Host "#14 CLEAN START FAIL - failures=$fail"
Write-Host "Report: $report"
exit 1
