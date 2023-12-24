Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

Write-Output "Downloading cloudflare go..."

Set-Location "$Env:Temp"

git clone -q https://github.com/cloudflare/go
Write-Output "Building go..."
cd go/src
# https://github.com/cloudflare/go/tree/34129e47042e214121b6bbff0ded4712debed18e is version go1.21.5-devel-cf
git checkout -q 34129e47042e214121b6bbff0ded4712debed18e
& ./make.bat

Write-Output "Installed"