# FinalCat cross-build (Windows host)
$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
$Out = if ($env:OUT) { $env:OUT } else { Join-Path $Root "dist" }
$ClientOut = Join-Path $Out "client"

New-Item -ItemType Directory -Force -Path $ClientOut | Out-Null
Set-Location $Root
go mod tidy

function Build-One($GoOS, $GoArch, $OutPath, $Pkg) {
    Write-Host "==> $GoOS/$GoArch -> $OutPath (CGO_ENABLED=0, static)"
    $env:CGO_ENABLED = "0"
    $env:GOOS = $GoOS
    $env:GOARCH = $GoArch
    go build -trimpath -ldflags="-s -w" -o $OutPath $Pkg
    Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue
    Remove-Item Env:GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
}

# Server: Linux + Windows, amd64 + 386
Build-One linux   amd64 (Join-Path $Out "server_linux_amd64")           "./cmd/server"
Build-One linux   386   (Join-Path $Out "server_linux_386")             "./cmd/server"
Build-One windows amd64 (Join-Path $Out "server_windows_amd64.exe")     "./cmd/server"
Build-One windows 386   (Join-Path $Out "server_windows_386.exe")       "./cmd/server"

# Client: Linux + Windows, amd64 + 386
Build-One linux   amd64 (Join-Path $ClientOut "agent_linux_amd64")           "./cmd/agent"
Build-One linux   386   (Join-Path $ClientOut "agent_linux_386")             "./cmd/agent"
Build-One windows amd64 (Join-Path $ClientOut "agent_windows_amd64.exe")     "./cmd/agent"
Build-One windows 386   (Join-Path $ClientOut "agent_windows_386.exe")       "./cmd/agent"

Write-Host "Done -> $Out"
Get-ChildItem $Out, $ClientOut | Format-Table Name, Length -AutoSize
