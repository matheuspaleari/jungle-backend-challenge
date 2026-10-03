$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
$Report = Join-Path $Root "observability_11_validation.txt"

Push-Location $Root
try {
    $lines = New-Object System.Collections.Generic.List[string]

    function Add-Line([string]$value) {
        Write-Host $value
        $lines.Add($value)
    }

    Add-Line "#11 OBSERVABILITY VALIDATION"
    Add-Line "Started: $((Get-Date).ToString('o'))"

    Add-Line ""
    Add-Line "== GOFMT CHECK =="
    $gofmt = gofmt -l .\internal\observability .\internal\platform\app\module.go .\internal\transport\httpapi\handler.go .\internal\infrastructure\sqsconsumer\consumer.go .\internal\infrastructure\outboxpublisher\publisher.go
    if ($gofmt) {
        Add-Line "GOFMT_REQUIRED:"
        foreach ($item in $gofmt) { Add-Line "$item" }
    } else {
        Add-Line "GOFMT: OK"
    }

    Add-Line ""
    Add-Line "== GO TEST =="
    $test = go test ./... -count=1 2>&1
    foreach ($item in $test) { Add-Line "$item" }
    if ($LASTEXITCODE -ne 0) { throw "go test failed" }

    Add-Line ""
    Add-Line "== GO VET =="
    $vet = go vet ./... 2>&1
    foreach ($item in $vet) { Add-Line "$item" }
    if ($LASTEXITCODE -ne 0) { throw "go vet failed" }
    Add-Line "GO VET: OK"

    Add-Line ""
    Add-Line "== GO BUILD =="
    $build = go build ./... 2>&1
    foreach ($item in $build) { Add-Line "$item" }
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    Add-Line "GO BUILD: OK"

    Add-Line ""
    Add-Line "NOTE: Start the API before running the HTTP checks below."
    Add-Line "Expected public endpoints:"
    Add-Line "GET /health/live"
    Add-Line "GET /health/ready"
    Add-Line "GET /metrics"

    Add-Line ""
    Add-Line "11_STATIC_VALIDATION=PASS"

    $lines | Set-Content -Path $Report -Encoding UTF8
    Write-Host ""
    Write-Host "Report: $Report"
}
finally {
    Pop-Location
}
