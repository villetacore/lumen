<#
.SYNOPSIS
    Поднимает сервер и веб-клиент Lumen для разработки.
.EXAMPLE
    ./scripts/dev.ps1
    ./scripts/dev.ps1 -Vault D:\notes -Addr 127.0.0.1:9000
#>
param(
    [string]$Vault = "./vault-example",
    [string]$Addr = "127.0.0.1:8787"
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

Write-Host "Vault : $Vault"
Write-Host "Server: http://$Addr"
Write-Host "Web   : http://localhost:5173"

$server = Start-Process -PassThru -WorkingDirectory "$root/server" `
    -FilePath "go" -ArgumentList @("run", "./cmd/lumend", "--vault", "$root/$Vault", "--addr", $Addr)

try {
    if (-not (Test-Path "$root/web/node_modules")) {
        Push-Location "$root/web"; npm install; Pop-Location
    }
    Push-Location "$root/web"
    npm run dev
} finally {
    Pop-Location
    if (-not $server.HasExited) { Stop-Process -Id $server.Id -Force }
}
