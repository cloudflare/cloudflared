Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$env:TARGET_OS = "windows"
$env:LOCAL_OS = "windows"

New-Item -Path ".\artifacts" -ItemType Directory

Write-Output "Building for amd64"
$env:TARGET_ARCH = "amd64"
$env:LOCAL_ARCH = "amd64"
$env:CGO_ENABLED = 1
& make cloudflared
if ($LASTEXITCODE -ne 0) { throw "Failed to build cloudflared for amd64" }
copy .\cloudflared.exe .\artifacts\cloudflared-windows-amd64.exe

Write-Output "Building for 386"
$env:TARGET_ARCH = "386"
$env:LOCAL_ARCH = "386"
$env:CGO_ENABLED = 0
& make cloudflared
if ($LASTEXITCODE -ne 0) { throw "Failed to build cloudflared for 386" }
copy .\cloudflared.exe .\artifacts\cloudflared-windows-386.exe
