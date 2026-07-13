# Run Go tests (Windows, no Docker)
# Usage: .\test.ps1

$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

Write-Host "go test ./..." -ForegroundColor Cyan
go test ./...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host "go vet ./..." -ForegroundColor Cyan
go vet ./...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host "go build ./cmd/api" -ForegroundColor Cyan
go build -o bin\api.exe ./cmd/api
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host "OK — tests, vet, and build passed." -ForegroundColor Green
