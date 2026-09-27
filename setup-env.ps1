<# 
.SYNOPSIS
    Generate and set environment variables for Local Model Router

.DESCRIPTION
    This script generates AUTH_ENCRYPTION_KEY and ADMIN_PASSWORD,
    sets them as environment variables, and optionally saves to .env file.

.PARAMETER AdminPassword
    Optional. Specify a custom admin password instead of generating one.

.PARAMETER Persistent
    Optional. Set environment variables persistently for the current user.

.EXAMPLE
    .\setup-env.ps1
    
.EXAMPLE
    .\setup-env.ps1 -AdminPassword "MySecurePassword123"
    
.EXAMPLE
    .\setup-env.ps1 -Persistent
#>

param(
    [Parameter(Mandatory=$false)]
    [string]$AdminPassword,
    
    [Parameter(Mandatory=$false)]
    [switch]$Persistent
)

Write-Host "Local Model Router - Environment Setup" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
Write-Host ""

# Function to generate random hex string
function Get-RandomHexString {
    param([int]$Length = 64)
    
    $bytes = New-Object byte[] ($Length / 2)
    $rng = [System.Security.Cryptography.RNGCryptoServiceProvider]::Create()
    $rng.GetBytes($bytes)
    $rng.Dispose()
    
    return ($bytes | ForEach-Object { $_.ToString("x2") }) -join ''
}

# Function to generate random password
function Get-RandomPassword {
    param([int]$Length = 16)
    
    $chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%"
    $bytes = New-Object byte[] $Length
    $rng = [System.Security.Cryptography.RNGCryptoServiceProvider]::Create()
    $rng.GetBytes($bytes)
    $rng.Dispose()
    
    $result = ""
    foreach ($byte in $bytes) {
        $result += $chars[$byte % $chars.Length]
    }
    return $result
}

# Generate AUTH_ENCRYPTION_KEY (32 bytes = 64 hex characters)
$AuthEncryptionKey = Get-RandomHexString -Length 64

# Generate or use provided ADMIN_PASSWORD
if ($AdminPassword) {
    $GeneratedPassword = $AdminPassword
    Write-Host "Using provided admin password" -ForegroundColor Yellow
} else {
    $GeneratedPassword = Get-RandomPassword -Length 16
}

# Set environment variables for current session
$env:AUTH_ENCRYPTION_KEY = $AuthEncryptionKey
$env:ADMIN_PASSWORD = $GeneratedPassword

# Display the values
Write-Host "Generated environment variables:" -ForegroundColor Green
Write-Host ""
Write-Host "AUTH_ENCRYPTION_KEY=" -NoNewline
Write-Host $AuthEncryptionKey -ForegroundColor Yellow
Write-Host "ADMIN_PASSWORD=" -NoNewline
Write-Host $GeneratedPassword -ForegroundColor Yellow
Write-Host ""

# Set persistent environment variables if requested
if ($Persistent) {
    [Environment]::SetEnvironmentVariable("AUTH_ENCRYPTION_KEY", $AuthEncryptionKey, "User")
    [Environment]::SetEnvironmentVariable("ADMIN_PASSWORD", $GeneratedPassword, "User")
    Write-Host "Environment variables set persistently for current user." -ForegroundColor Green
    Write-Host "You may need to restart your terminal for changes to take effect." -ForegroundColor Yellow
    Write-Host ""
}

# Prompt to save to .env file
$SaveToFile = Read-Host "Save to .env file? (y/N)"
if ($SaveToFile -eq 'y' -or $SaveToFile -eq 'Y') {
    $envContent = @"
# Local Model Router Environment Variables
# Generated on $(Get-Date)
# WARNING: Keep this file secure and never commit to version control!

AUTH_ENCRYPTION_KEY=$AuthEncryptionKey
ADMIN_PASSWORD=$GeneratedPassword

# Optional: Ollama endpoint (uncomment to override default)
# OLLAMA_ENDPOINT=http://localhost:11434
"@
    
    $envContent | Out-File -FilePath ".env" -Encoding UTF8
    Write-Host "Saved to .env file" -ForegroundColor Green
}

Write-Host ""
Write-Host "Environment variables have been set for the current PowerShell session." -ForegroundColor Green
Write-Host ""
Write-Host "To start the router:" -ForegroundColor Cyan
Write-Host "  .\bin\local-model-router.exe -config config.example.yaml"
Write-Host ""
Write-Host "Or with Docker Compose:" -ForegroundColor Cyan
Write-Host "  docker-compose up -d"
Write-Host ""

# Return values as object for programmatic use
return @{
    AUTH_ENCRYPTION_KEY = $AuthEncryptionKey
    ADMIN_PASSWORD = $GeneratedPassword
}
