Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$WorkingDirectory = Get-Location
$CloudflaredDirectory = "$WorkingDirectory\go\src\github.com\cloudflare\cloudflared"

Write-Output "Installing python..."

$PythonVersion = "3.10.11"
$PythonZipFile = "$env:Temp\python-$PythonVersion-embed-amd64.zip"
$PipInstallFile = "$env:Temp\get-pip.py"
$PythonZipUrl = "https://www.python.org/ftp/python/$PythonVersion/python-$PythonVersion-embed-amd64.zip"
$PythonPath = "$WorkingDirectory\Python"
$PythonBinPath = "$PythonPath\python.exe"

# Download Python zip file
Invoke-WebRequest -Uri $PythonZipUrl -OutFile $PythonZipFile

# Download Python pip file
Invoke-WebRequest -Uri "https://bootstrap.pypa.io/get-pip.py" -OutFile $PipInstallFile

# Extract Python files
Expand-Archive $PythonZipFile -DestinationPath $PythonPath -Force

# Add Python to PATH
$env:Path = "$PythonPath\Scripts;$PythonPath;$($env:Path)"

Write-Output "Installed to $PythonPath"

# Install pip
& $PythonBinPath $PipInstallFile

# Add package paths in pythonXX._pth to unblock python -m pip
$PythonImportPathFile = "$PythonPath\python310._pth"
$ComponentTestsDir = "$CloudflaredDirectory\component-tests\"
@($ComponentTestsDir, "Lib\site-packages", $(Get-Content $PythonImportPathFile)) | Set-Content $PythonImportPathFile

# Test Python installation
& $PythonBinPath --version
& $PythonBinPath -m pip --version

go env
go version

$env:TARGET_OS = "windows"
$env:CGO_ENABLED = 1
$env:TARGET_ARCH = "amd64"
$env:Path = "$Env:Temp\go\bin;$($env:Path)"

& $PythonBinPath --version
& $PythonBinPath -m pip --version

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

& $PythonBinPath -m pip install --upgrade -r component-tests/requirements.txt
& $PythonBinPath component-tests/setup.py --type create
& $PythonBinPath -m pytest component-tests -o log_cli=true --log-cli-level=INFO
if ($LASTEXITCODE -ne 0) {
    & $PythonBinPath component-tests/setup.py --type cleanup
    throw "Failed component tests"
}
& $PythonBinPath component-tests/setup.py --type cleanup