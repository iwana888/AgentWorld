<#
.SYNOPSIS
  AgentWorld one-shot build script: embed the Vue frontend into the Go binary.
.DESCRIPTION
  1. cd web/ and run npm install + npm run build -> outputs to ../webstatic/dist
     (frontend includes mobile-responsive layout: sidebar -> bottom tab bar on <=768px)
  2. cd root and run go build; Go embeds webstatic/dist via //go:embed
  3. Produces bin/agentworld.exe (single self-contained file, no extra dirs)
#>

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path

function Step($msg) { Write-Host ""; Write-Host "==> $msg" -ForegroundColor Cyan }

# 1. Build frontend
Step "Build frontend (npm run build -> webstatic/dist)"
Push-Location (Join-Path $root "web")
try {
    if (-not (Test-Path "node_modules")) {
        Write-Host "  Installing deps: npm install ..."
        npm install
    } else {
        Write-Host "  node_modules exists, skipping npm install"
    }
    npm run build
    if ($LASTEXITCODE -ne 0) {
        throw "Frontend build failed (exit=$LASTEXITCODE)"
    }
    if (-not (Test-Path "..\webstatic\dist\index.html")) {
        throw "Frontend build failed: webstatic/dist/index.html not found"
    }
}
finally {
    Pop-Location
}

# 2. Compile Go (embeds frontend)
Step "Compile Go binary (embed webstatic/dist)"
$out = Join-Path $root "bin/agentworld.exe"
if (-not (Test-Path (Join-Path $root "bin"))) {
    New-Item -ItemType Directory -Path (Join-Path $root "bin") | Out-Null
}
Push-Location $root
try {
    go build -o $out .
    if ($LASTEXITCODE -ne 0) {
        throw "Go build failed (exit=$LASTEXITCODE)"
    }
    if (-not (Test-Path $out)) {
        throw "Output not generated: $out"
    }
}
finally {
    Pop-Location
}

Step "Build complete"
Write-Host "  Output: $out" -ForegroundColor Green
Write-Host "  Run:    $out   (default http://localhost:18080)" -ForegroundColor Green

# 3. 附带配置文件示例到 bin/（部署时复制为 config.toml 即可）
Step "Copy config example to bin/"
$ex = Join-Path $root "config.toml.example"
if (Test-Path $ex) {
    Copy-Item -Force $ex (Join-Path $root "bin/config.toml.example")
    Write-Host "  Copied config.toml.example -> bin/ (复制为 config.toml 并放 exe 同目录生效)" -ForegroundColor Green
}
