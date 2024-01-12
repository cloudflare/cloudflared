Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

# Relative path to working directory
$CloudflaredDirectory = "go\src\github.com\cloudflare\cloudflared"

cd $CloudflaredDirectory

Write-Output "Building for amd64"
$env:TARGET_OS = "windows"
$env:CGO_ENABLED = 1
$env:TARGET_ARCH = "amd64"
$env:Path = "$Env:Temp\go\bin;$($env:Path)"

go env
go version

& make cloudflared
if ($LASTEXITCODE -ne 0) { throw "Failed to build cloudflared for amd64" }
copy .\cloudflared.exe .\cloudflared-windows-amd64.exe

Write-Output "Building for 386"
$env:CGO_ENABLED = 0
$env:TARGET_ARCH = "386"
make cloudflared
if ($LASTEXITCODE -ne 0) { throw "Failed to build cloudflared for 386" }
copy .\cloudflared.exe .\cloudflared-windows-386.exe