$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$report = Join-Path $root "distributed_12_e2e.txt"
$binDir = Join-Path $root ".tmp-12"
$binary = Join-Path $binDir "jungle-api-12.exe"
$ports = @(18081, 18082, 18083)
$processes = @()
$failures = 0

function Log([string]$Text = "") { $Text | Tee-Object -FilePath $report -Append }
function Pass([string]$Text) { Log "[PASS] $Text" }
function Fail([string]$Text) { $script:failures++; Log "[FAIL] $Text" }
function Assert-Eq($Actual, $Expected, [string]$Text) {
    if ([string]$Actual -eq [string]$Expected) { Pass "$Text | esperado=$Expected | obtido=$Actual" }
    else { Fail "$Text | esperado=$Expected | obtido=$Actual" }
}
function Headers([string]$Token, [string]$Idem = "") {
    $h = @{ Authorization = "Bearer $Token" }
    if ($Idem) { $h["Idempotency-Key"] = $Idem }
    return $h
}
function Api([string]$Method, [string]$Url, [hashtable]$Headers, $Body = $null) {
    $params = @{ Method=$Method; Uri=$Url; Headers=$Headers; UseBasicParsing=$true }
    if ($null -ne $Body) { $params.ContentType="application/json"; $params.Body=($Body | ConvertTo-Json -Depth 12 -Compress) }
    $r = Invoke-WebRequest @params
    return @{ Status=[int]$r.StatusCode; Json=($r.Content | ConvertFrom-Json); Raw=$r.Content }
}
function Start-Instance([int]$Port) {
    $env:HTTP_PORT = [string]$Port
    $env:DATABASE_URL = "postgres://jungle:jungle@localhost:5432/jungle?sslmode=disable"
    $stdout = Join-Path $binDir "api-$Port.out.log"
    $stderr = Join-Path $binDir "api-$Port.err.log"
    return Start-Process -FilePath $binary -PassThru -WindowStyle Hidden -RedirectStandardOutput $stdout -RedirectStandardError $stderr
}
function Wait-Ready([int]$Port) {
    for ($i=0; $i -lt 40; $i++) {
        try {
            $r = Invoke-RestMethod "http://localhost:$Port/health/ready" -TimeoutSec 2
            if ($r.status -eq "ready") { return }
        } catch {}
        Start-Sleep -Milliseconds 500
    }
    throw "API $Port did not become ready"
}
function New-Wallet([int]$Port, [string]$Token, [string]$Player) {
    return Api "POST" "http://localhost:$Port/wallets" (Headers $Token) @{ playerId=$Player; initialBalance=@{amount="100.00";currency="BRL"} }
}
function WagerBody([string]$Provider,[string]$External,[string]$Player,[string]$Wallet,[string]$Amount,[string]$Round) {
    return @{ providerId=$Provider; externalTransactionId=$External; playerId=$Player; walletId=$Wallet; roundId=$Round; gameId="distributed-12"; kind="BET"; money=@{amount=$Amount;currency="BRL"} }
}
function Parallel-Wagers($Requests, [string]$Token) {
    $jobs = @()
    foreach ($r in $Requests) {
        $jobs += Start-Job -ScriptBlock {
            param($Url,$Token,$Idem,$Json)
            try {
                $resp = Invoke-WebRequest -UseBasicParsing -Method POST -Uri $Url -Headers @{Authorization="Bearer $Token";"Idempotency-Key"=$Idem} -ContentType "application/json" -Body $Json
                [pscustomobject]@{Status=[int]$resp.StatusCode;Body=$resp.Content;Error=""}
            } catch {
                $status = 0
                if ($_.Exception.Response) { $status = [int]$_.Exception.Response.StatusCode }
                [pscustomobject]@{Status=$status;Body="";Error=$_.Exception.Message}
            }
        } -ArgumentList $r.Url,$Token,$r.Idem,($r.Body | ConvertTo-Json -Depth 12 -Compress)
    }
    $results = $jobs | Wait-Job | Receive-Job
    $jobs | Remove-Job -Force
    return @($results)
}

try {
    "#12 DISTRIBUTED E2E" | Set-Content $report
    Log "Started: $(Get-Date -Format o)"
    Log ""
    Log "== BUILD + THREE REAL OS PROCESSES =="
    New-Item -ItemType Directory -Force -Path $binDir | Out-Null
    Push-Location $root
    & go build -o $binary ./cmd/api
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    Pop-Location

    foreach ($p in $ports) { $processes += Start-Instance $p }
    foreach ($p in $ports) { Wait-Ready $p }
    $pids = @($processes | ForEach-Object { $_.Id } | Select-Object -Unique)
    Log "PIDs: $($pids -join ', ')"
    Assert-Eq $pids.Count 3 "Tres processos de aplicacao independentes"

    Log ""
    Log "== KEYCLOAK TOKEN =="
    $tokenResponse = Invoke-RestMethod -Method POST -Uri "http://localhost:8081/realms/jungle/protocol/openid-connect/token" -ContentType "application/x-www-form-urlencoded" -Body @{grant_type="client_credentials";client_id="provider-a";client_secret="provider-a-secret"}
    $token = $tokenResponse.access_token
    if ($token) { Pass "Token provider-a obtido" } else { Fail "Token provider-a ausente" }

    $suffix = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()

    Log ""
    Log "== 50 IDENTICAL BETS IN PARALLEL / ONE DEBIT =="
    $player50 = "player-12-50-$suffix"
    $wallet50Resp = New-Wallet 18081 $token $player50
    Assert-Eq $wallet50Resp.Status 201 "Wallet do teste 50 criada"
    $wallet50 = $wallet50Resp.Json.id
    $external50 = "bet-12-50-$suffix"
    $idem50 = "provider-a:$external50"
    $body50 = WagerBody "provider-a" $external50 $player50 $wallet50 "10.00" "round-50-$suffix"
    $requests50 = @()
    for ($i=0; $i -lt 50; $i++) {
        $port = $ports[$i % 3]
        $requests50 += @{Url="http://localhost:$port/wagering/transactions";Idem=$idem50;Body=$body50}
    }
    $results50 = Parallel-Wagers $requests50 $token
    Assert-Eq $results50.Count 50 "50 respostas recebidas"
    Assert-Eq (@($results50 | Where-Object {$_.Status -eq 200}).Count) 50 "50 requests retornam 200"
    $json50 = @($results50 | ForEach-Object { if ($_.Body) { $_.Body | ConvertFrom-Json } })
    Assert-Eq (@($json50 | Where-Object {$_.idempotentReplay -eq $false}).Count) 1 "Apenas um processamento original"
    Assert-Eq (@($json50 | Where-Object {$_.idempotentReplay -eq $true}).Count) 49 "49 replays idempotentes"
    $final50 = Api "GET" "http://localhost:18082/wallets/$wallet50" (Headers $token)
    Assert-Eq $final50.Json.balance.amount "90.00" "Saldo final apos 50 requests"
    Assert-Eq $final50.Json.version 2 "Wallet version mudou uma unica vez"
    $ledger50 = Api "GET" "http://localhost:18083/wallets/$wallet50/ledger?limit=50" (Headers $token)
    Assert-Eq (@($ledger50.Json.entries).Count) 2 "Ledger contem OPENING + um unico DEBIT"

    Log ""
    Log "== TWO DISTINCT 80 BETS AGAINST 100 =="
    $player80 = "player-12-80-$suffix"
    $wallet80 = (New-Wallet 18082 $token $player80).Json.id
    $bodyA = WagerBody "provider-a" "bet-12-80-a-$suffix" $player80 $wallet80 "80.00" "round-80-$suffix"
    $bodyB = WagerBody "provider-a" "bet-12-80-b-$suffix" $player80 $wallet80 "80.00" "round-80-$suffix"
    $r80 = Parallel-Wagers @(
        @{Url="http://localhost:18081/wagering/transactions";Idem="provider-a:bet-12-80-a-$suffix";Body=$bodyA},
        @{Url="http://localhost:18083/wagering/transactions";Idem="provider-a:bet-12-80-b-$suffix";Body=$bodyB}
    ) $token
    $j80 = @($r80 | ForEach-Object { if ($_.Body) { $_.Body | ConvertFrom-Json } })
    Assert-Eq (@($j80 | Where-Object {$_.status -eq "PROCESSED"}).Count) 1 "Uma BET 80 processada"
    Assert-Eq (@($j80 | Where-Object {$_.status -eq "REJECTED" -and $_.failureCode -eq "INSUFFICIENT_FUNDS"}).Count) 1 "Uma BET 80 rejeitada por saldo"
    $final80 = Api "GET" "http://localhost:18082/wallets/$wallet80" (Headers $token)
    Assert-Eq $final80.Json.balance.amount "20.00" "Saldo final 20.00"
    $ledger80 = Api "GET" "http://localhost:18082/wallets/$wallet80/ledger?limit=50" (Headers $token)
    Assert-Eq (@($ledger80.Json.entries).Count) 2 "Apenas um debit financeiro no ledger"

    Log ""
    Log "== INDEPENDENT WALLETS IN PARALLEL =="
    $pA="player-12-independent-a-$suffix"; $pB="player-12-independent-b-$suffix"
    $wA=(New-Wallet 18081 $token $pA).Json.id; $wB=(New-Wallet 18082 $token $pB).Json.id
    $ind = Parallel-Wagers @(
        @{Url="http://localhost:18081/wagering/transactions";Idem="provider-a:ind-a-$suffix";Body=(WagerBody "provider-a" "ind-a-$suffix" $pA $wA "10.00" "round-ind-a-$suffix")},
        @{Url="http://localhost:18083/wagering/transactions";Idem="provider-a:ind-b-$suffix";Body=(WagerBody "provider-a" "ind-b-$suffix" $pB $wB "10.00" "round-ind-b-$suffix")}
    ) $token
    Assert-Eq (@($ind | Where-Object {$_.Status -eq 200}).Count) 2 "Wallets independentes processam em paralelo"
    $finalA = Api "GET" "http://localhost:18082/wallets/$wA" (Headers $token)
    $finalB = Api "GET" "http://localhost:18082/wallets/$wB" (Headers $token)
    Assert-Eq $finalA.Json.balance.amount "90.00" "Wallet A final"
    Assert-Eq $finalB.Json.balance.amount "90.00" "Wallet B final"

    Log ""
    Log "== RESTART + DURABLE IDEMPOTENCY =="
    Stop-Process -Id $processes[2].Id -Force
    $processes[2].WaitForExit()
    $processes[2] = Start-Instance 18083
    Wait-Ready 18083
    Pass "Terceira instancia reiniciada com novo PID $($processes[2].Id)"
    $replay = Api "POST" "http://localhost:18083/wagering/transactions" (Headers $token $idem50) $body50
    Assert-Eq $replay.Json.idempotentReplay $true "Replay continua idempotente apos restart"
    $afterRestart = Api "GET" "http://localhost:18083/wallets/$wallet50" (Headers $token)
    Assert-Eq $afterRestart.Json.balance.amount "90.00" "Restart nao altera saldo"
    Assert-Eq $afterRestart.Json.version 2 "Restart nao altera version"

    Log ""
    Log "== METRICS =="
    $metrics = (Invoke-WebRequest -UseBasicParsing "http://localhost:18081/metrics").Content
    ($metrics -split "`n" | Where-Object {$_ -match '^jungle_'}) | ForEach-Object { Log $_ }

    Log ""
    Log "Falhas encontradas: $failures"
    if ($failures -eq 0) { Log "12_DISTRIBUTED_E2E_RESULT=PASS" } else { Log "12_DISTRIBUTED_E2E_RESULT=FAIL"; exit 1 }
}
finally {
    foreach ($p in $processes) {
        try { if ($p -and -not $p.HasExited) { Stop-Process -Id $p.Id -Force } } catch {}
    }
    Remove-Item Env:HTTP_PORT -ErrorAction SilentlyContinue
}
