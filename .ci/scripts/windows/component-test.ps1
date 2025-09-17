Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$env:TARGET_OS = "windows"
$env:LOCAL_OS = "windows"
$env:TARGET_ARCH = "amd64"
$env:LOCAL_ARCH = "amd64"
$env:CGO_ENABLED = 1

python --version
python -m pip --version


Write-Host "Building cloudflared"
& make cloudflared
if ($LASTEXITCODE -ne 0) { throw "Failed to build cloudflared" }


Write-Host "Running unit tests"
# Not testing with race detector because of https://github.com/golang/go/issues/61058
# We already test it on other platforms
go test -failfast -v -mod=vendor ./...
if ($LASTEXITCODE -ne 0) { throw "Failed unit tests" }


# On Gitlab runners we need to add all of this addresses to the NO_PROXY list in order for the tests to run.
$env:NO_PROXY = "pypi.org,files.pythonhosted.org,api.cloudflare.com,argotunneltest.com,argotunnel.com,trycloudflare.com,${env:NO_PROXY}"
Write-Host "No Proxy: ${env:NO_PROXY}"
Write-Host "Running component tests"
try {
    python -m pip --disable-pip-version-check install --upgrade -r component-tests/requirements.txt --use-pep517
    python component-tests/setup.py --type create
    python -m pytest component-tests -o log_cli=true --log-cli-level=INFO --junit-xml=report.xml
    if ($LASTEXITCODE -ne 0) {
        throw "Failed component tests"
    }
} finally {
    python component-tests/setup.py --type cleanup
}
