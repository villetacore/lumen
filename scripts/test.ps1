<#
.SYNOPSIS
    Прогоняет все проверки проекта: Go, Rust, TypeScript.
#>
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

Push-Location "$root/server";      go test ./...;      Pop-Location
Push-Location "$root/desktop/core"; cargo test;        Pop-Location
Push-Location "$root/web";          npx tsc -b --noEmit; Pop-Location
if (Get-Command flutter -ErrorAction SilentlyContinue) {
    & "$root/scripts/desktop.ps1" -Test
} else {
    Write-Host "Flutter не найден — десктопные тесты пропущены" -ForegroundColor Yellow
}

Write-Host "Все проверки пройдены" -ForegroundColor Green
