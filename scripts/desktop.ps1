<#
.SYNOPSIS
    Собирает и запускает десктопный клиент Lumen (Flutter + Windows).
.DESCRIPTION
    Перед вызовом Flutter чистит PATH внутри своего процесса. Это обход
    известной проблемы: если в PATH есть некорректная запись, flutter.bat
    пишет в stderr сообщение об ошибке, а tool_backend.dart из Flutter SDK
    декодирует этот поток строгим UTF-8. На локализованной Windows сообщение
    приходит в cp1251, декодер падает с FormatException, и сборка
    заканчивается MSB8066 / кодом 255 без внятной причины.

    Систему скрипт не меняет: PATH правится только для текущего процесса.
    Как починить PATH навсегда — в desktop/app/README.md.
.EXAMPLE
    ./scripts/desktop.ps1            # собрать релиз
    ./scripts/desktop.ps1 -Run       # собрать и запустить
    ./scripts/desktop.ps1 -Test      # анализ и тесты
    ./scripts/desktop.ps1 -Debug     # отладочная сборка
#>
param(
    [switch]$Run,
    [switch]$Test,
    [switch]$Debug
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$app = Join-Path $root "desktop/app"

if (-not (Get-Command flutter -ErrorAction SilentlyContinue)) {
    throw "Flutter не найден в PATH. Установка: https://docs.flutter.dev/get-started/install/windows/desktop"
}

# Записи с '<', '>' или '%' некорректны и ломают разбор PATH в cmd.
$bad = $env:Path -split ';' | Where-Object { $_ -and $_ -match '[<>%]' }
if ($bad) {
    Write-Host "PATH содержит некорректные записи, обхожу их на время сборки:" -ForegroundColor Yellow
    $bad | ForEach-Object { Write-Host "  $_" -ForegroundColor DarkYellow }
    Write-Host "  Как починить навсегда — desktop/app/README.md" -ForegroundColor DarkYellow
}
$env:Path = ($env:Path -split ';' | Where-Object { $_ -and $_ -notmatch '[<>%]' } | Select-Object -Unique) -join ';'

# Запущенное приложение держит exe и ломает шаг INSTALL с невнятным MSB3073.
$running = Get-Process lumen_app -ErrorAction SilentlyContinue
if ($running) {
    Write-Host "Закрываю запущенный lumen_app (иначе сборка не сможет заменить exe)" -ForegroundColor Yellow
    $running | Stop-Process -Force
    Start-Sleep -Milliseconds 500
}

Push-Location $app
try {
    flutter pub get

    if ($Test) {
        flutter analyze
        flutter test
        Write-Host "`nПроверки пройдены" -ForegroundColor Green
        return
    }

    $mode = if ($Debug) { "--debug" } else { "--release" }

    if ($Run) {
        Write-Host "`nЗапускаю. Нужен работающий lumend на 127.0.0.1:8787:" -ForegroundColor Cyan
        Write-Host "  dist\lumend.exe --vault .\vault-example`n" -ForegroundColor DarkCyan
        flutter run -d windows $mode
        return
    }

    flutter build windows $mode
    $sub = if ($Debug) { "Debug" } else { "Release" }
    $exe = Join-Path $app "build/windows/x64/runner/$sub/lumen_app.exe"
    Write-Host "`nГотово: $exe" -ForegroundColor Green
    Write-Host "Перед запуском подними сервер: dist\lumend.exe --vault .\vault-example"
} finally {
    Pop-Location
}
