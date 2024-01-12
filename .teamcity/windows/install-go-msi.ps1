$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
$GoMsiVersion = "go1.21.5.windows-amd64.msi"
 
Write-Output "Downloading go installer..."
 
Set-Location "$Env:Temp"
 
(New-Object System.Net.WebClient).DownloadFile(
    "https://go.dev/dl/$GoMsiVersion",
    "$Env:Temp\$GoMsiVersion"
)
 
Write-Output "Installing go..."
Install-Package "$Env:Temp\$GoMsiVersion" -Force
 
# Go installer updates global $PATH
go env
 
Write-Output "Installed"