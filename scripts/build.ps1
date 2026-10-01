<#
.SYNOPSIS
    Собирает Lumen в один бинарник со вшитым веб-интерфейсом.
.DESCRIPTION
    Веб-клиент компилируется Vite, копируется в server/internal/webui/dist и
    вшивается в бинарник через go:embed. Результат — dist/lumend(.exe),
    которому не нужны ни node, ни отдельный фронтенд.
.EXAMPLE
    ./scripts/build.ps1
    ./scripts/build.ps1 -Version 0.2.0
#>
param(
    [string]$Version = "0.1.0-dev"
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$uiDist = Join-Path $root "server/internal/webui/dist"

Write-Host "1/3 сборка веб-клиента" -ForegroundColor Cyan
Push-Location (Join-Path $root "web")
try {
    if (-not (Test-Path "node_modules")) { npm install --no-fund --no-audit }
    npm run build
} finally { Pop-Location }

Write-Host "2/3 копирование ассетов в бинарник" -ForegroundColor Cyan
if (Test-Path $uiDist) { Remove-Item -Recurse -Force $uiDist }
New-Item -ItemType Directory -Force -Path $uiDist | Out-Null
Copy-Item -Recurse -Force (Join-Path $root "web/dist/*") $uiDist
Set-Content -Encoding utf8 -Path (Join-Path $uiDist ".gitkeep") `
    -Value "Сюда scripts/build.ps1 копирует собранный веб-клиент из web/dist."

Write-Host "3/3 сборка сервера" -ForegroundColor Cyan
$out = Join-Path $root "dist/lumend.exe"
New-Item -ItemType Directory -Force -Path (Join-Path $root "dist") | Out-Null
Push-Location (Join-Path $root "server")
try {
    go build -ldflags "-X main.version=$Version" -o $out ./cmd/lumend
} finally { Pop-Location }

Write-Host "`nГотово: $out" -ForegroundColor Green
Write-Host "Запуск: dist\lumend.exe --vault .\vault-example --open"
