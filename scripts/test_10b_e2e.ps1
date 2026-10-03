$ErrorActionPreference = "Stop"

$ApiBase = "http://localhost:8080"
$KeycloakBase = "http://localhost:8081"
$Realm = "jungle"

$OutputFile = Join-Path $PSScriptRoot "..\oidc_10b_e2e.txt"

$script:Results = @()
$script:Failures = 0

function Log {
    param([string]$Text)

    Write-Host $Text
    $script:Results += $Text
}

function Section {
    param([string]$Title)

    Log ""
    Log ("=" * 90)
    Log $Title
    Log ("=" * 90)
}

function Pass {
    param([string]$Message)

    Log "[PASS] $Message"
}

function Fail {
    param([string]$Message)

    $script:Failures++
    Log "[FAIL] $Message"
}

function Assert-Equal {
    param(
        [string]$Name,
        $Actual,
        $Expected
    )

    if ("$Actual" -eq "$Expected") {
        Pass "$Name | esperado=$Expected | obtido=$Actual"
    }
    else {
        Fail "$Name | esperado=$Expected | obtido=$Actual"
    }
}

function Get-AccessToken {
    param(
        [string]$ClientId,
        [string]$ClientSecret
    )

    $response = Invoke-RestMethod `
        -Method Post `
        -Uri "$KeycloakBase/realms/$Realm/protocol/openid-connect/token" `
        -ContentType "application/x-www-form-urlencoded" `
        -Body @{
            grant_type    = "client_credentials"
            client_id     = $ClientId
            client_secret = $ClientSecret
        }

    if ([string]::IsNullOrWhiteSpace($response.access_token)) {
        throw "Keycloak nao retornou access_token para $ClientId"
    }

    return $response.access_token
}

function Invoke-TestRequest {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Method,

        [Parameter(Mandatory = $true)]
        [string]$Uri,

        [string]$Token,

        [object]$Body,

        [hashtable]$ExtraHeaders
    )

    $headers = @{}

    if (-not [string]::IsNullOrWhiteSpace($Token)) {
        $headers["Authorization"] = "Bearer $Token"
    }

    if ($null -ne $ExtraHeaders) {
        foreach ($key in $ExtraHeaders.Keys) {
            $headers[$key] = $ExtraHeaders[$key]
        }
    }

    $params = @{
        Method          = $Method
        Uri             = $Uri
        Headers         = $headers
        UseBasicParsing = $true
    }

    if ($null -ne $Body) {
        $params["ContentType"] = "application/json"
        $params["Body"] = ($Body | ConvertTo-Json -Depth 20 -Compress)
    }

    try {
        $response = Invoke-WebRequest @params

        $parsedBody = $null

        if (-not [string]::IsNullOrWhiteSpace($response.Content)) {
            try {
                $parsedBody = $response.Content | ConvertFrom-Json
            }
            catch {
                $parsedBody = $response.Content
            }
        }

        return [PSCustomObject]@{
            Status = [int]$response.StatusCode
            Body   = $parsedBody
            Raw    = $response.Content
        }
    }
    catch {
        if ($null -eq $_.Exception.Response) {
            throw
        }

        $response = $_.Exception.Response
        $status = [int]$response.StatusCode

        $raw = ""

        if ($_.ErrorDetails -and $_.ErrorDetails.Message) {
            $raw = $_.ErrorDetails.Message
        }

        $parsedBody = $null

        if (-not [string]::IsNullOrWhiteSpace($raw)) {
            try {
                $parsedBody = $raw | ConvertFrom-Json
            }
            catch {
                $parsedBody = $raw
            }
        }

        return [PSCustomObject]@{
            Status = $status
            Body   = $parsedBody
            Raw    = $raw
        }
    }
}

function Show-Response {
    param(
        [string]$Name,
        $Response
    )

    Log "$Name STATUS: $($Response.Status)"

    if ($null -ne $Response.Body) {
        Log "$Name BODY:"
        Log ($Response.Body | ConvertTo-Json -Depth 20 -Compress)
    }
}

# ==============================================================================
# INICIO
# ==============================================================================

Section "#10B - OIDC + PROVIDER ISOLATION E2E"

Log "API      : $ApiBase"
Log "Keycloak : $KeycloakBase"
Log "Inicio   : $((Get-Date).ToString('o'))"

# ==============================================================================
# 1. TOKENS REAIS
# ==============================================================================

Section "1. KEYCLOAK - CLIENT CREDENTIALS"

try {
    $TokenA = Get-AccessToken `
        -ClientId "provider-a" `
        -ClientSecret "provider-a-secret"

    Pass "Token provider-a obtido"
}
catch {
    Fail "Nao foi possivel obter token provider-a: $($_.Exception.Message)"
    throw
}

try {
    $TokenB = Get-AccessToken `
        -ClientId "provider-b" `
        -ClientSecret "provider-b-secret"

    Pass "Token provider-b obtido"
}
catch {
    Fail "Nao foi possivel obter token provider-b: $($_.Exception.Message)"
    throw
}

# ==============================================================================
# 2. AUTHENTICATION
# ==============================================================================

Section "2. AUTENTICACAO"

$NoToken = Invoke-TestRequest `
    -Method "GET" `
    -Uri "$ApiBase/wallets/00000000-0000-0000-0000-000000000001"

Show-Response "SEM TOKEN" $NoToken
Assert-Equal "Sem token deve retornar 401" $NoToken.Status 401

$InvalidToken = Invoke-TestRequest `
    -Method "GET" `
    -Uri "$ApiBase/wallets/00000000-0000-0000-0000-000000000001" `
    -Token "token-invalido"

Show-Response "TOKEN INVALIDO" $InvalidToken
Assert-Equal "Token invalido deve retornar 401" $InvalidToken.Status 401

# ==============================================================================
# 3. PROVIDER-A CRIA WALLET
# ==============================================================================

Section "3. PROVIDER-A CRIA WALLET"

$Suffix = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()

$PlayerA = "player-oidc-10b-$Suffix"

$OpenWalletBody = @{
    playerId = $PlayerA
    initialBalance = @{
        amount   = "100.00"
        currency = "BRL"
    }
}

$CreateWallet = Invoke-TestRequest `
    -Method "POST" `
    -Uri "$ApiBase/wallets" `
    -Token $TokenA `
    -Body $OpenWalletBody

Show-Response "CREATE WALLET A" $CreateWallet
Assert-Equal "Provider A cria wallet" $CreateWallet.Status 201

if ($CreateWallet.Status -ne 201 -or
    $null -eq $CreateWallet.Body -or
    [string]::IsNullOrWhiteSpace($CreateWallet.Body.id)) {

    Fail "Nao foi possivel obter walletId; testes dependentes nao podem continuar"
    throw "wallet creation failed"
}

$WalletID = $CreateWallet.Body.id

Log "WALLET_ID: $WalletID"
Log "PLAYER_ID: $PlayerA"

Assert-Equal `
    "Saldo inicial" `
    $CreateWallet.Body.balance.amount `
    "100.00"

Assert-Equal `
    "Wallet version inicial" `
    $CreateWallet.Body.version `
    1

# ==============================================================================
# 4. OWNERSHIP DA WALLET
# ==============================================================================

Section "4. WALLET OWNERSHIP"

$WalletA = Invoke-TestRequest `
    -Method "GET" `
    -Uri "$ApiBase/wallets/$WalletID" `
    -Token $TokenA

Show-Response "A GET WALLET A" $WalletA
Assert-Equal "Provider A acessa propria wallet" $WalletA.Status 200

$WalletB = Invoke-TestRequest `
    -Method "GET" `
    -Uri "$ApiBase/wallets/$WalletID" `
    -Token $TokenB

Show-Response "B GET WALLET A" $WalletB
Assert-Equal "Provider B nao acessa wallet de A" $WalletB.Status 403

# ==============================================================================
# 5. LEDGER ISOLATION
# ==============================================================================

Section "5. LEDGER ISOLATION"

$LedgerA = Invoke-TestRequest `
    -Method "GET" `
    -Uri "$ApiBase/wallets/$WalletID/ledger?limit=50" `
    -Token $TokenA

Show-Response "A LEDGER A" $LedgerA
Assert-Equal "Provider A acessa ledger" $LedgerA.Status 200

$LedgerB = Invoke-TestRequest `
    -Method "GET" `
    -Uri "$ApiBase/wallets/$WalletID/ledger?limit=50" `
    -Token $TokenB

Show-Response "B LEDGER A" $LedgerB
Assert-Equal "Provider B nao acessa ledger de A" $LedgerB.Status 403

# ==============================================================================
# 6. RECONCILIATION ISOLATION
# ==============================================================================

Section "6. RECONCILIATION ISOLATION"

$ReconciliationA = Invoke-TestRequest `
    -Method "POST" `
    -Uri "$ApiBase/wallets/$WalletID/reconciliation" `
    -Token $TokenA

Show-Response "A RECONCILIATION A" $ReconciliationA
Assert-Equal "Provider A executa reconciliation" $ReconciliationA.Status 200

if ($ReconciliationA.Status -eq 200) {
    Assert-Equal `
        "Reconciliation consistente antes do BET" `
        $ReconciliationA.Body.consistent `
        $true

    Assert-Equal `
        "Diferenca antes do BET" `
        $ReconciliationA.Body.difference.amount `
        "0.00"
}

$ReconciliationB = Invoke-TestRequest `
    -Method "POST" `
    -Uri "$ApiBase/wallets/$WalletID/reconciliation" `
    -Token $TokenB

Show-Response "B RECONCILIATION A" $ReconciliationB
Assert-Equal "Provider B nao reconcilia wallet de A" $ReconciliationB.Status 403

# ==============================================================================
# 7. PROVIDER-A PROCESSA BET
# ==============================================================================

Section "7. BET AUTORIZADO - PROVIDER-A"

$ExternalTxA = "oidc-10b-bet-a-$Suffix"
$IdempotencyA = "provider-a:$ExternalTxA"

$BetARequest = @{
    providerId            = "provider-a"
    externalTransactionId = $ExternalTxA
    playerId              = $PlayerA
    walletId              = $WalletID
    roundId               = "round-oidc-10b-$Suffix"
    gameId                = "fortune-chimp"
    kind                  = "BET"
    money = @{
        amount   = "10.00"
        currency = "BRL"
    }
}

$BetA = Invoke-TestRequest `
    -Method "POST" `
    -Uri "$ApiBase/wagering/transactions" `
    -Token $TokenA `
    -ExtraHeaders @{
        "Idempotency-Key" = $IdempotencyA
    } `
    -Body $BetARequest

Show-Response "A BET A" $BetA
Assert-Equal "BET autorizado retorna 200" $BetA.Status 200

if ($BetA.Status -eq 200) {
    Assert-Equal "BET status" $BetA.Body.status "PROCESSED"
    Assert-Equal "Saldo apos BET" $BetA.Body.balance.amount "90.00"
    Assert-Equal "Primeiro processamento nao e replay" $BetA.Body.idempotentReplay $false
}

$TransactionID = $BetA.Body.transactionId

if ([string]::IsNullOrWhiteSpace($TransactionID)) {
    Fail "BET processado nao retornou transactionId"
}
else {
    Log "TRANSACTION_ID: $TransactionID"
}

# ==============================================================================
# 8. IDEMPOTENT REPLAY
# ==============================================================================

Section "8. IDEMPOTENT REPLAY"

$BetReplay = Invoke-TestRequest `
    -Method "POST" `
    -Uri "$ApiBase/wagering/transactions" `
    -Token $TokenA `
    -ExtraHeaders @{
        "Idempotency-Key" = $IdempotencyA
    } `
    -Body $BetARequest

Show-Response "A BET REPLAY" $BetReplay
Assert-Equal "Replay retorna 200" $BetReplay.Status 200

if ($BetReplay.Status -eq 200) {
    Assert-Equal "Replay permanece PROCESSED" $BetReplay.Body.status "PROCESSED"
    Assert-Equal "Replay identificado" $BetReplay.Body.idempotentReplay $true
    Assert-Equal "Replay retorna saldo original do processamento" $BetReplay.Body.balance.amount "90.00"
    Assert-Equal "Replay retorna mesma transactionId" $BetReplay.Body.transactionId $TransactionID
}

# ==============================================================================
# 9. PROVIDER-B TENTA USAR WALLET DE A
# ==============================================================================

Section "9. CROSS-PROVIDER FINANCIAL ATTACK"

$ExternalTxB = "oidc-10b-cross-provider-$Suffix"

$BetBRequest = @{
    providerId            = "provider-b"
    externalTransactionId = $ExternalTxB
    playerId              = $PlayerA
    walletId              = $WalletID
    roundId               = "round-cross-$Suffix"
    gameId                = "fortune-chimp"
    kind                  = "BET"
    money = @{
        amount   = "10.00"
        currency = "BRL"
    }
}

$BetB = Invoke-TestRequest `
    -Method "POST" `
    -Uri "$ApiBase/wagering/transactions" `
    -Token $TokenB `
    -ExtraHeaders @{
        "Idempotency-Key" = "provider-b:$ExternalTxB"
    } `
    -Body $BetBRequest

Show-Response "B BET WALLET A" $BetB
Assert-Equal "Provider B nao movimenta wallet de A" $BetB.Status 403

# ==============================================================================
# 10. TOKEN A MENTINDO PROVIDER NO PAYLOAD
# ==============================================================================

Section "10. TOKEN/PAYLOAD PROVIDER MISMATCH"

$FakeExternal = "oidc-10b-fake-provider-$Suffix"

$FakeProviderRequest = @{
    providerId            = "provider-b"
    externalTransactionId = $FakeExternal
    playerId              = $PlayerA
    walletId              = $WalletID
    roundId               = "round-fake-$Suffix"
    gameId                = "fortune-chimp"
    kind                  = "BET"
    money = @{
        amount   = "10.00"
        currency = "BRL"
    }
}

$FakeProvider = Invoke-TestRequest `
    -Method "POST" `
    -Uri "$ApiBase/wagering/transactions" `
    -Token $TokenA `
    -ExtraHeaders @{
        "Idempotency-Key" = "provider-b:$FakeExternal"
    } `
    -Body $FakeProviderRequest

Show-Response "TOKEN A / PAYLOAD B" $FakeProvider
Assert-Equal "Token A nao pode declarar provider B" $FakeProvider.Status 403

# ==============================================================================
# 11. TRANSACTION READ ISOLATION
# ==============================================================================

Section "11. TRANSACTION READ ISOLATION"

if (-not [string]::IsNullOrWhiteSpace($TransactionID)) {

    $TransactionA = Invoke-TestRequest `
        -Method "GET" `
        -Uri "$ApiBase/wagering/transactions/$TransactionID" `
        -Token $TokenA

    Show-Response "A GET TX A" $TransactionA
    Assert-Equal "Provider A acessa propria transacao" $TransactionA.Status 200

    if ($TransactionA.Status -eq 200) {
        Assert-Equal "Transacao pertence ao provider-a" $TransactionA.Body.providerId "provider-a"
        Assert-Equal "Transacao pertence a wallet correta" $TransactionA.Body.walletId $WalletID
    }

    $TransactionB = Invoke-TestRequest `
        -Method "GET" `
        -Uri "$ApiBase/wagering/transactions/$TransactionID" `
        -Token $TokenB

    Show-Response "B GET TX A" $TransactionB
    Assert-Equal "Provider B nao acessa transacao interna de A" $TransactionB.Status 403
}

# ==============================================================================
# 12. PROVIDER + EXTERNAL TRANSACTION LOOKUP
# ==============================================================================

Section "12. PROVIDER/EXTERNAL TRANSACTION LOOKUP"

$ProviderLookupA = Invoke-TestRequest `
    -Method "GET" `
    -Uri "$ApiBase/providers/provider-a/wagering/transactions/$ExternalTxA" `
    -Token $TokenA

Show-Response "A PROVIDER LOOKUP A" $ProviderLookupA
Assert-Equal "Provider A consulta externalTransactionId" $ProviderLookupA.Status 200

$ProviderLookupB = Invoke-TestRequest `
    -Method "GET" `
    -Uri "$ApiBase/providers/provider-a/wagering/transactions/$ExternalTxA" `
    -Token $TokenB

Show-Response "B PROVIDER LOOKUP A" $ProviderLookupB
Assert-Equal "Provider B nao consulta namespace de A" $ProviderLookupB.Status 403

# ==============================================================================
# 13. SALDO FINAL
# ==============================================================================

Section "13. FINAL FINANCIAL STATE"

$FinalWallet = Invoke-TestRequest `
    -Method "GET" `
    -Uri "$ApiBase/wallets/$WalletID" `
    -Token $TokenA

Show-Response "FINAL WALLET" $FinalWallet
Assert-Equal "Wallet final acessivel por A" $FinalWallet.Status 200

if ($FinalWallet.Status -eq 200) {
    Assert-Equal "Saldo final deve ser 90.00" $FinalWallet.Body.balance.amount "90.00"
    Assert-Equal "Version final deve ser 2" $FinalWallet.Body.version 2
}

# ==============================================================================
# 14. LEDGER FINAL
# ==============================================================================

Section "14. FINAL LEDGER"

$FinalLedger = Invoke-TestRequest `
    -Method "GET" `
    -Uri "$ApiBase/wallets/$WalletID/ledger?limit=50" `
    -Token $TokenA

Show-Response "FINAL LEDGER" $FinalLedger
Assert-Equal "Ledger final acessivel" $FinalLedger.Status 200

if ($FinalLedger.Status -eq 200) {
    $Entries = @($FinalLedger.Body.entries)

    Log "LEDGER ENTRY COUNT: $($Entries.Count)"

    Assert-Equal `
        "Ledger deve conter OPENING + BET" `
        $Entries.Count `
        2
}

# ==============================================================================
# 15. RECONCILIATION FINAL
# ==============================================================================

Section "15. FINAL RECONCILIATION"

$FinalReconciliation = Invoke-TestRequest `
    -Method "POST" `
    -Uri "$ApiBase/wallets/$WalletID/reconciliation" `
    -Token $TokenA

Show-Response "FINAL RECONCILIATION" $FinalReconciliation
Assert-Equal "Reconciliation final retorna 200" $FinalReconciliation.Status 200

if ($FinalReconciliation.Status -eq 200) {
    Assert-Equal "Stored balance final" $FinalReconciliation.Body.storedBalance.amount "90.00"
    Assert-Equal "Calculated balance final" $FinalReconciliation.Body.calculatedBalance.amount "90.00"
    Assert-Equal "Difference final" $FinalReconciliation.Body.difference.amount "0.00"
    Assert-Equal "Reconciliation final consistente" $FinalReconciliation.Body.consistent $true
    Assert-Equal "Reconciliation verifica 2 entradas" $FinalReconciliation.Body.checkedEntries 2
}

# ==============================================================================
# 16. POSTGRES OWNERSHIP PROOF
# ==============================================================================

Section "16. POSTGRES OWNERSHIP PROOF"

try {
    $DbProof = docker exec jungle-postgres `
        psql -U jungle -d jungle -t -A `
        -c "SELECT id::text || '|' || COALESCE(provider_id, '<NULL>') || '|' || player_id || '|' || currency || '|' || balance_minor::text || '|' || version::text FROM wallets WHERE id = '$WalletID';"

    $DbProof = "$DbProof".Trim()

    Log "DB WALLET: $DbProof"

    $DbParts = $DbProof -split "\|"

    if ($DbParts.Count -eq 6) {
        Assert-Equal "DB provider_id persistido" $DbParts[1] "provider-a"
        Assert-Equal "DB player_id persistido" $DbParts[2] $PlayerA
        Assert-Equal "DB currency persistida" $DbParts[3] "BRL"
        Assert-Equal "DB balance_minor final" $DbParts[4] "9000"
        Assert-Equal "DB wallet version final" $DbParts[5] "2"
    }
    else {
        Fail "Formato inesperado da prova PostgreSQL: $DbProof"
    }
}
catch {
    Fail "Falha ao consultar ownership diretamente no PostgreSQL: $($_.Exception.Message)"
}

# ==============================================================================
# RESUMO
# ==============================================================================

Section "RESULTADO FINAL"

Log "Wallet testada     : $WalletID"
Log "Player             : $PlayerA"
Log "External TX        : $ExternalTxA"
Log "Transaction ID     : $TransactionID"
Log "Falhas encontradas : $script:Failures"
Log "Fim                : $((Get-Date).ToString('o'))"

if ($script:Failures -eq 0) {
    Log ""
    Log "10B_E2E_RESULT=PASS"
    Log "TODOS OS TESTES DE ISOLAMENTO PASSARAM."
}
else {
    Log ""
    Log "10B_E2E_RESULT=FAIL"
    Log "$script:Failures TESTE(S) FALHARAM."
}

$script:Results | Set-Content `
    -Path $OutputFile `
    -Encoding UTF8

Write-Host ""
Write-Host "Relatorio salvo em:"
Write-Host $OutputFile

if ($script:Failures -gt 0) {
    exit 1
}

exit 0