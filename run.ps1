# Run SACAS API with normal Go commands (Windows, no Docker).
# Usage (from Sacas-backend folder):
#   .\run.ps1
#   .\run.ps1 -SkipTidy

param(
    [switch]$SkipTidy
)

$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "ERROR: 'go' not found on PATH." -ForegroundColor Red
    Write-Host "Install:  winget install GoLang.Go"
    Write-Host "Then open a NEW terminal and retry."
    exit 1
}

if (-not (Test-Path ".env")) {
    if (Test-Path ".env.example") {
        Copy-Item ".env.example" ".env"
        Write-Host "Created .env from .env.example — edit DATABASE_URL / password if needed." -ForegroundColor Yellow
    } else {
        Write-Host "ERROR: no .env or .env.example found." -ForegroundColor Red
        exit 1
    }
}

Write-Host "Go: $(go version)" -ForegroundColor Cyan
if (-not $SkipTidy) {
    Write-Host "go mod tidy ..." -ForegroundColor Cyan
    go mod tidy
}

Write-Host "Starting DEV server: go run ." -ForegroundColor Green
Write-Host "  Health: http://localhost:8080/api/health"
Write-Host "  Admin:  admin@example.com / password"
Write-Host "  Guide:  see HOW_TO_USE.md"
Write-Host "  Tip: leave SOLVER_URL empty in .env (no Python solver needed)." -ForegroundColor DarkGray
Write-Host ""

go run .
