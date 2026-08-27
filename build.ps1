# OpenVpnTools Windows 构建脚本
# 用法:
#   .\build.ps1           # 前端 + 交叉编译 linux/amd64 与 linux/arm64
#   .\build.ps1 -SkipWeb  # 跳过前端构建
param(
    [switch]$SkipWeb
)
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

if (-not $SkipWeb) {
    Write-Host "==> 构建前端 (web/dist)" -ForegroundColor Cyan
    Push-Location web
    npm install --no-fund --no-audit
    npm run build
    Pop-Location
}

Write-Host "==> go vet + go test" -ForegroundColor Cyan
go vet ./...
go test ./...

New-Item -ItemType Directory -Force dist | Out-Null

Write-Host "==> 交叉编译 linux/amd64" -ForegroundColor Cyan
$env:CGO_ENABLED = '0'; $env:GOOS = 'linux'; $env:GOARCH = 'amd64'
go build -trimpath -ldflags "-s -w" -o dist/ovpn-web-linux-amd64 ./cmd/ovpn-web

Write-Host "==> 交叉编译 linux/arm64" -ForegroundColor Cyan
$env:GOARCH = 'arm64'
go build -trimpath -ldflags "-s -w" -o dist/ovpn-web-linux-arm64 ./cmd/ovpn-web

Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue

Write-Host "==> 本机调试版 bin/ovpn-web.exe" -ForegroundColor Cyan
go build -o bin/ovpn-web.exe ./cmd/ovpn-web

Get-ChildItem dist | Format-Table Name, Length
Write-Host "完成。部署:把 dist/ovpn-web-linux-amd64 与 scripts/ 拷到服务器,运行 install-panel.sh" -ForegroundColor Green
