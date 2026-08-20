# Run Pascal World A/B/C experiment (Experience -> Behavior)
# Usage (from AgentWorld root):
#   .\run_abc.ps1
# Requires env LLM_API_KEY (or fill it in below).
#
# IMPORTANT: Pascal entrypoint is worlds/pascal/cmd/pascal, NOT repo root main.go.
# This script cd's there automatically to avoid starting the whole web service.
#
# Each group runs 10 issues:
#   - progress logs shown live on screen (you see what it is doing)
#   - clean JSON written to abc_A.json / abc_B.json / abc_C.json
#   - full logs (incl. errors) written to abc_A.log / abc_B.log / abc_C.log
# Off-peak hours (0-9 / 12-14 / 18-24) are cheaper (DeepSeek peak/valley pricing).

$ErrorActionPreference = "Stop"

# ---- config ----
if (-not $env:LLM_API_KEY) {
    # fill directly if you do not want to set the env var each time:
    # $env:LLM_API_KEY = "sk-xxx"
    Write-Error "Please set env LLM_API_KEY first"
    exit 1
}

# Pascal entrypoint dir (must run go run here, else the root service starts)
$pascalCmd = Join-Path $PSScriptRoot "worlds/pascal/cmd/pascal"
if (-not (Test-Path $pascalCmd)) {
    Write-Error "Pascal entry dir not found: $pascalCmd"
    exit 1
}

$env:PASCAL_USE_WSL = "1"
$env:LLM_BASE_URL   = if ($env:LLM_BASE_URL) { $env:LLM_BASE_URL } else { "https://api.deepseek.com/v1" }
$env:LLM_MODEL      = if ($env:LLM_MODEL) { $env:LLM_MODEL } else { "deepseek-v4-flash" }

$groups = @("A", "B", "C")

Push-Location $pascalCmd
try {
    foreach ($g in $groups) {
        $jsonPath = Join-Path $PSScriptRoot "abc_$g.json"
        $logPath  = Join-Path $PSScriptRoot "abc_$g.log"
        Write-Host "=== [$(Get-Date -Format 'HH:mm:ss')] Running Group $g ===" -ForegroundColor Cyan
        Write-Host "    JSON -> $jsonPath" -ForegroundColor DarkGray
        Write-Host "    LOG  -> $logPath" -ForegroundColor DarkGray

        # live output to screen + save to log; JSON written separately
        go run . --abc $g --abc-json $jsonPath *>&1 | Tee-Object -FilePath $logPath
        if ($LASTEXITCODE -ne 0) {
            Write-Error "Group $g failed (exit $LASTEXITCODE), see $logPath"
            exit $LASTEXITCODE
        }
        Write-Host "[$(Get-Date -Format 'HH:mm:ss')] Group $g done" -ForegroundColor Green
        Write-Host ""
    }
} finally {
    Pop-Location
}

Write-Host "All done. A/B/C results in abc_A.json / abc_B.json / abc_C.json" -ForegroundColor Green
