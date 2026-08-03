param(
    [int]$Port = 8080,
    [string]$Password = "mock-photobook"
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
$mockData = Join-Path $projectRoot ".mock-data"

$previousAddress = $env:PHOTOBOOK_ADDRESS
$previousDataDirectory = $env:PHOTOBOOK_DATA_DIR
$previousPassword = $env:PHOTOBOOK_ADMIN_PASSWORD
$previousMockUpdates = $env:PHOTOBOOK_MOCK_UPDATES

Push-Location $projectRoot
try {
    $env:PHOTOBOOK_ADDRESS = "127.0.0.1:$Port"
    $env:PHOTOBOOK_DATA_DIR = $mockData
    $env:PHOTOBOOK_ADMIN_PASSWORD = $Password
    $env:PHOTOBOOK_MOCK_UPDATES = "true"

    Write-Host "PhotoBook Windows mock"
    Write-Host "Admin:   http://localhost:$Port/admin/"
    Write-Host "Display: http://localhost:$Port/display/"
    Write-Host "Username: admin"
    Write-Host "Password: $Password"
    Write-Host "Test photos are stored in $mockData"
    Write-Host "Press Ctrl+C to stop."

    $mockVersion = (git rev-parse --short=12 HEAD 2>$null)
    if (-not $mockVersion) { $mockVersion = "dev" }
    go run -ldflags="-X main.buildVersion=windows-mock-$mockVersion" .
}
finally {
    $env:PHOTOBOOK_ADDRESS = $previousAddress
    $env:PHOTOBOOK_DATA_DIR = $previousDataDirectory
    $env:PHOTOBOOK_ADMIN_PASSWORD = $previousPassword
    $env:PHOTOBOOK_MOCK_UPDATES = $previousMockUpdates
    Pop-Location
}
