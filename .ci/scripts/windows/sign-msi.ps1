# Sign Windows artifacts using azuretool
# This script processes MSI files from the artifacts directory

$ErrorActionPreference = "Stop"

# Define paths
$ARTIFACT_DIR = "artifacts"
$TIMESTAMP_RFC3161 = "http://timestamp.digicert.com"

Write-Host "Looking for Windows artifacts to sign in $ARTIFACT_DIR..."

# Find all Windows MSI files
$msiFiles = Get-ChildItem -Path $ARTIFACT_DIR -Filter "cloudflared-windows-*.msi" -ErrorAction SilentlyContinue

if ($msiFiles.Count -eq 0) {
    Write-Host "No Windows MSI files found in $ARTIFACT_DIR"
    exit 1
}

Write-Host "Found $($msiFiles.Count) file(s) to sign:"
foreach ($file in $msiFiles) {
    Write-Host "Running azuretool sign for $($file.Name)"
    azuresigntool.exe sign -kvu $env:KEY_VAULT_URL -kvi "$env:KEY_VAULT_CLIENT_ID" -kvs "$env:KEY_VAULT_SECRET" -kvc "$env:KEY_VAULT_CERTIFICATE" -kvt "$env:KEY_VAULT_TENANT_ID" -tr "$TIMESTAMP_RFC3161" -d "Cloudflare Tunnel Daemon" .\\$ARTIFACT_DIR\\$($file.Name)
}

Write-Host "Signing process completed"
