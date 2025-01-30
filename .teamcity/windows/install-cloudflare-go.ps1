Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

Write-Output "Downloading cloudflare go..."

Set-Location "$Env:Temp"

git clone -q https://github.com/cloudflare/go
Write-Output "Building go..."
cd go/src
# https://github.com/cloudflare/go/tree/af19da5605ca11f85776ef7af3384a02a315a52b is version go1.22.5-devel-cf
git checkout -q af19da5605ca11f85776ef7af3384a02a315a52b
& ./make.bat

Write-Output "Installed"
