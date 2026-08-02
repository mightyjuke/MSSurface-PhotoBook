param(
    [string]$OutputDirectory = "dist"
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
$outputPath = Join-Path $projectRoot $OutputDirectory
New-Item -ItemType Directory -Force -Path $outputPath | Out-Null

$previousGoos = $env:GOOS
$previousGoarch = $env:GOARCH
$previousGoarm = $env:GOARM
$previousCgo = $env:CGO_ENABLED

try {
    $env:GOOS = "linux"
    $env:GOARCH = "arm"
    $env:GOARM = "7"
    $env:CGO_ENABLED = "0"
    $buildVersion = (git -C $projectRoot rev-parse --short=12 HEAD 2>$null)
    if (-not $buildVersion) { $buildVersion = "dev" }
    go build -trimpath -ldflags="-s -w -X main.buildVersion=$buildVersion" -o (Join-Path $outputPath "photobook-armv7") $projectRoot
    if ($LASTEXITCODE -ne 0) { throw "Go build failed" }
}
finally {
    $env:GOOS = $previousGoos
    $env:GOARCH = $previousGoarch
    $env:GOARM = $previousGoarm
    $env:CGO_ENABLED = $previousCgo
}

$artifact = Get-Item (Join-Path $outputPath "photobook-armv7")
Write-Host "Built $($artifact.FullName) ($([math]::Round($artifact.Length / 1MB, 1)) MB)"
