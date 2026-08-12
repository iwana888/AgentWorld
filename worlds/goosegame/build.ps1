# build.ps1 -- Build a single-file GooseGame executable (frontend embedded).
#
# Steps:
#   1. cd worlds/goosegame/web && npm install && npm run build  -> outputs to ../webstatic/dist
#   2. cd repo root && go build  -> worlds/goosegame/webstatic pkg embeds dist via //go:embed
#
# Output: <repo root>/bin/goose.exe (Windows) or bin/goose (Linux/macOS).
# Run bin/goose.exe then open http://localhost:19090 (frontend + API same origin).

$ErrorActionPreference = "Stop"
Set-Location "$PSScriptRoot\web"

Write-Host "[1/2] Building frontend -> webstatic/dist ..." -ForegroundColor Cyan
if (-not (Test-Path node_modules)) { npm install }
npm run build

Write-Host "[2/2] Building single-file Go binary -> bin/goose.exe ..." -ForegroundColor Cyan
Set-Location "$PSScriptRoot\..\.."
New-Item -ItemType Directory -Force -Path "bin" | Out-Null
go build -o bin/goose.exe ./worlds/goosegame/cmd/goose

Write-Host "Done: bin/goose.exe (frontend embedded, single file)" -ForegroundColor Green
Write-Host "Run: bin\goose.exe, then open http://localhost:19090" -ForegroundColor Green
