$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$report = Join-Path $root "documentation_13_validation.txt"

function Pass([string]$Message) {
    "[PASS] $Message" | Tee-Object -FilePath $report -Append
}

function Require-File([string]$Path) {
    if (-not (Test-Path $Path -PathType Leaf)) {
        throw "Arquivo obrigatorio ausente: $Path"
    }
    Pass "Arquivo existe: $Path"
}

function Require-Text([string]$Path, [string]$Pattern, [string]$Label) {
    $content = Get-Content -Raw $Path
    if ($content -notmatch $Pattern) {
        throw "Conteudo obrigatorio ausente em ${Path}: $Label"
    }
    Pass "$Label"
}

"#13 DOCUMENTATION VALIDATION" | Set-Content $report
"Started: $(Get-Date -Format o)" | Add-Content $report

Require-File ".\README.md"
Require-File ".\ARCHITECTURE.md"
Require-File ".\docs\REQUIREMENTS_MATRIX.md"
Require-File ".\scripts\validate_12.ps1"
Require-File ".\scripts\test_12_distributed.ps1"

Require-Text ".\README.md" "Idempotência" "README documenta idempotencia"
Require-Text ".\README.md" "Transactional Outbox" "README documenta outbox"
Require-Text ".\README.md" "OAuth2/OIDC" "README documenta OIDC"
Require-Text ".\README.md" "test_12_distributed\.ps1" "README usa nome real do teste distribuido"
Require-Text ".\ARCHITECTURE.md" "Inbox ReceiveTx" "Arquitetura documenta Inbox atomica"
Require-Text ".\ARCHITECTURE.md" "eventId" "Arquitetura documenta recuperacao da Outbox"
Require-Text ".\docs\REQUIREMENTS_MATRIX.md" "Pontos obrigatórios do #14" "Matriz preserva pendencias para auditoria final"

"`r`n== GO TEST ==" | Add-Content $report
cmd.exe /d /c "go test ./... -count=1 2>&1" | Tee-Object -FilePath $report -Append
if ($LASTEXITCODE -ne 0) { throw "go test falhou" }
Pass "go test ./..."

"`r`n== GO VET ==" | Add-Content $report
cmd.exe /d /c "go vet ./... 2>&1" | Tee-Object -FilePath $report -Append
if ($LASTEXITCODE -ne 0) { throw "go vet falhou" }
Pass "go vet ./..."

"`r`n== GO BUILD ==" | Add-Content $report
cmd.exe /d /c "go build ./cmd/api 2>&1" | Tee-Object -FilePath $report -Append
if ($LASTEXITCODE -ne 0) { throw "go build falhou" }
Pass "go build ./cmd/api"

"`r`n13_DOCUMENTATION_RESULT=PASS" | Add-Content $report
Write-Host ""
Write-Host "#13 documentation validation PASS"
Write-Host "Report: $report"
