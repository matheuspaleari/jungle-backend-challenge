$ErrorActionPreference = "Stop"
$output = ".\oidc_10b_e2e.txt"
$tokenUrl = "http://localhost:8081/realms/jungle/protocol/openid-connect/token"
$api = "http://localhost:8080"

"=== #10B PROVIDER ISOLATION E2E ===" | Set-Content $output

function Token($client, $secret) {
    (Invoke-RestMethod -Method Post -Uri $tokenUrl -ContentType "application/x-www-form-urlencoded" -Body @{
        grant_type="client_credentials"; client_id=$client; client_secret=$secret
    }).access_token
}
function Capture($name, [scriptblock]$call) {
    "`n=== $name ===" | Add-Content $output
    try {
        $r = & $call
        "HTTP=SUCCESS" | Add-Content $output
        if ($null -ne $r) { $r | ConvertTo-Json -Depth 10 | Add-Content $output }
    } catch {
        $status = -1
        try { $status = [int]$_.Exception.Response.StatusCode } catch {}
        "HTTP=$status" | Add-Content $output
        "ERROR=$($_.Exception.Message)" | Add-Content $output
    }
}

$tokenA = Token "provider-a" "provider-a-secret"
$tokenB = Token "provider-b" "provider-b-secret"
$stamp = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
$player = "player-isolation-$stamp"

$walletBody = @{playerId=$player; initialBalance=@{amount="100.00";currency="BRL"}} | ConvertTo-Json -Depth 5
$wallet = Invoke-RestMethod -Method Post -Uri "$api/wallets" -Headers @{Authorization="Bearer $tokenA"} -ContentType "application/json" -Body $walletBody
$walletId = $wallet.id
"walletId=$walletId" | Add-Content $output

Capture "1 PROVIDER-A GET OWN WALLET" { Invoke-RestMethod -Method Get -Uri "$api/wallets/$walletId" -Headers @{Authorization="Bearer $tokenA"} }
Capture "2 PROVIDER-B GET WALLET A EXPECT 403" { Invoke-RestMethod -Method Get -Uri "$api/wallets/$walletId" -Headers @{Authorization="Bearer $tokenB"} }
Capture "3 PROVIDER-B LEDGER WALLET A EXPECT 403" { Invoke-RestMethod -Method Get -Uri "$api/wallets/$walletId/ledger" -Headers @{Authorization="Bearer $tokenB"} }
Capture "4 PROVIDER-B RECONCILE WALLET A EXPECT 403" { Invoke-RestMethod -Method Post -Uri "$api/wallets/$walletId/reconciliation" -Headers @{Authorization="Bearer $tokenB"} }

$extA = "isolation-bet-$stamp"
$betA = @{providerId="provider-a";externalTransactionId=$extA;playerId=$player;walletId=$walletId;roundId="round-$stamp";gameId="fortune-chimp";kind="BET";money=@{amount="10.00";currency="BRL"}} | ConvertTo-Json -Depth 6
$txA = Invoke-RestMethod -Method Post -Uri "$api/wagering/transactions" -Headers @{Authorization="Bearer $tokenA";"Idempotency-Key"="provider-a:$extA"} -ContentType "application/json" -Body $betA
$txId = $txA.transactionId
"transactionId=$txId" | Add-Content $output

Capture "5 PROVIDER-A GET OWN TRANSACTION" { Invoke-RestMethod -Method Get -Uri "$api/wagering/transactions/$txId" -Headers @{Authorization="Bearer $tokenA"} }
Capture "6 PROVIDER-B GET TRANSACTION A EXPECT 403" { Invoke-RestMethod -Method Get -Uri "$api/wagering/transactions/$txId" -Headers @{Authorization="Bearer $tokenB"} }

$extB = "isolation-cross-$stamp"
$betB = @{providerId="provider-b";externalTransactionId=$extB;playerId=$player;walletId=$walletId;roundId="round-$stamp";gameId="fortune-chimp";kind="BET";money=@{amount="10.00";currency="BRL"}} | ConvertTo-Json -Depth 6
Capture "7 PROVIDER-B BET ON WALLET A EXPECT 403" { Invoke-RestMethod -Method Post -Uri "$api/wagering/transactions" -Headers @{Authorization="Bearer $tokenB";"Idempotency-Key"="provider-b:$extB"} -ContentType "application/json" -Body $betB }

Capture "8 PROVIDER-A FINAL WALLET EXPECT 90" { Invoke-RestMethod -Method Get -Uri "$api/wallets/$walletId" -Headers @{Authorization="Bearer $tokenA"} }

"`n=== DATABASE OWNERSHIP ===" | Add-Content $output
docker exec jungle-postgres psql -U jungle -d jungle -c "SELECT id, provider_id, player_id, currency, balance_minor, version FROM wallets WHERE id = '$walletId';" 2>&1 | Add-Content $output

Write-Host "Generated $output"
