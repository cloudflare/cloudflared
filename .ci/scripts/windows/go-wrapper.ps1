Param(
    [string]$GoVersion,
    [string]$ScriptToExecute
)

# The script is a wrapper that downloads a specific version
# of go, adds it to the PATH and executes a script with that go
# version in the path.

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

# Get the path to the system's temporary directory.
$tempPath = [System.IO.Path]::GetTempPath()

# Create a unique name for the new temporary folder.
$folderName = "go_" + (Get-Random)

# Join the temp path and the new folder name to create the full path.
$fullPath = Join-Path -Path $tempPath -ChildPath $folderName

# Store the current value of PATH environment variable.
$oldPath = $env:Path

# Use a try...finally block to ensure the temporrary folder and PATH are cleaned up.
try {
    # Create the temporary folder.
    Write-Host "Creating temporary folder at: $fullPath"
    $newTempFolder = New-Item -ItemType Directory -Path $fullPath -Force

    # Download go
    $url = "https://go.dev/dl/$GoVersion.windows-amd64.zip"
    $destinationFile = Join-Path -Path $newTempFolder.FullName -ChildPath "go$GoVersion.windows-amd64.zip"
    Write-Host "Downloading go from: $url"
    Invoke-WebRequest -Uri $url -OutFile $destinationFile
    Write-Host "File downloaded to: $destinationFile"

    # Unzip the downloaded file.
    Write-Host "Unzipping the file..."
    Expand-Archive -Path $destinationFile -DestinationPath $newTempFolder.FullName -Force
    Write-Host "File unzipped successfully."

    # Define the go/bin path wich is inside the temporary folder
    $goBinPath = Join-Path -Path $fullPath -ChildPath "go\bin"

    # Add the go/bin path to the PATH environment variable.
    $env:Path = "$goBinPath;$($env:Path)"
    Write-Host "Added $goBinPath to the environment PATH."

    go env
    go version

    & $ScriptToExecute
} finally {
    # Cleanup: Remove the path from the environment variable and then the temporary folder.
    Write-Host "Starting cleanup..."

    $env:Path = $oldPath
    Write-Host "Reverted changes in the environment PATH."

    # Remove the temporary folder and its contents.
    if (Test-Path -Path $fullPath) {
        Remove-Item -Path $fullPath -Recurse -Force
        Write-Host "Temporary folder and its contents have been removed."
    } else {
        Write-Host "Temporary folder does not exist, no cleanup needed."
    }
}
