# build.ps1 -- Build a single-file Economy World executable (frontend embedded).
#
# Steps:
#   1. cd worlds/economy/web && npm install && npm run build  -> outputs to ../webstatic/dist
#   2. cd repo root && go build  -> worlds/economy/webstatic pkg embeds dist via //go:embed
#
# Output: <repo root>/bin/economy.exe (Windows) or bin/economy (Linux/macOS).
# Run bin/economy.exe then open http://localhost:19100 (frontend + API same origin).

$ErrorActionPreference = "Stop"
Set-Location "$PSScriptRoot\web"

Write-Host "[1/2] Building frontend -> webstatic/dist ..." -ForegroundColor Cyan
if (-not (Test-Path node_modules)) { npm install }
npm run build

Write-Host "[2/2] Building single-file Go binary -> bin/economy.exe ..." -ForegroundColor Cyan
Set-Location "$PSScriptRoot\..\.."
New-Item -ItemType Directory -Force -Path "bin" | Out-Null
go build -o bin/economy.exe ./worlds/economy/cmd/economy

Write-Host "Done: bin/economy.exe (frontend embedded, single file)" -ForegroundColor Green
Write-Host "Run: bin\economy.exe, then open http://localhost:19100" -ForegroundColor Green
