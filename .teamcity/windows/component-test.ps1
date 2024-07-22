Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$WorkingDirectory = Get-Location
$CloudflaredDirectory = "$WorkingDirectory\go\src\github.com\cloudflare\cloudflared"

go env
go version

$env:TARGET_OS = "windows"
$env:CGO_ENABLED = 1
$env:TARGET_ARCH = "amd64"
$env:Path = "$Env:Temp\go\bin;$($env:Path)"

python --version
python -m pip --version

cd $CloudflaredDirectory

go env
go version

Write-Output "Building cloudflared"

& make cloudflared
if ($LASTEXITCODE -ne 0) { throw "Failed to build cloudflared" }

echo $LASTEXITCODE

Write-Output "Running unit tests"

# Not testing with race detector because of https://github.com/golang/go/issues/61058
# We already test it on other platforms
& go test -failfast -mod=vendor ./...
if ($LASTEXITCODE -ne 0) { throw "Failed unit tests" }

Write-Output "Running component tests"

python -m pip --disable-pip-version-check install --upgrade -r component-tests/requirements.txt
python component-tests/setup.py --type create
python -m pytest component-tests -o log_cli=true --log-cli-level=INFO
if ($LASTEXITCODE -ne 0) {
    python component-tests/setup.py --type cleanup
    throw "Failed component tests"
}
python component-tests/setup.py --type cleanup