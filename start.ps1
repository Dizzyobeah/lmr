<#
.SYNOPSIS
    Build, sync models, and start the Local Model Router stack (Option A).

.DESCRIPTION
    One-command bootstrap for the Local Model Router:
      1. Preflight checks (Docker, .env, host Ollama store).
      2. Resolve the model list (defaults to every model on the host).
      3. Clean-rebuild the router image (lands the array-content fix needed
         for OpenCode plan mode).
      4. Sync the resolved models into the container's Ollama volume.
      5. Start the stack and refresh Ollama so it sees the new models.
      6. Verify health and print status.

    This keeps the fast native-ext4 Docker volume for Ollama (Option A) while
    mirroring the host's models into the container so routing rules that
    reference them (e.g. llava:latest, qwen3.6:latest) resolve.

.PARAMETER Models
    Model names to sync (bare names, e.g. gemma4,dolphin3). When omitted, every
    model reported by the host `ollama list` is synced.

.PARAMETER NoCache
    Build the router image with --no-cache. On by default so the committed
    array-content fix is guaranteed to be compiled in. Use -NoCache:$false for
    faster rebuilds once the binary is current.

.PARAMETER SkipBuild
    Skip rebuilding the router image entirely (fast restart / sync-only).

.PARAMETER HostOllama
    Path to the host Ollama store that sync-models.ps1 copies from.

.EXAMPLE
    .\start.ps1

.EXAMPLE
    .\start.ps1 -Models gemma4,dolphin3,llava,qwen3.6

.EXAMPLE
    .\start.ps1 -SkipBuild
#>
param(
    [string[]]$Models,
    [switch]$NoCache = $true,
    [switch]$SkipBuild,
    [string]$HostOllama = "C:/Users/kalle/.ollama"
)

$ErrorActionPreference = "Stop"
Set-Location -LiteralPath $PSScriptRoot

function Write-Step($msg) { Write-Host "`n==> $msg" -ForegroundColor Cyan }

# ------------------------------------------------------------------
# 1. Preflight
# ------------------------------------------------------------------
Write-Step "Preflight checks"

docker info *> $null
if (-not $?) { throw "Docker daemon not reachable. Start Docker Desktop and retry." }

if (-not (Test-Path ".env")) {
    throw ".env not found. Run ./setup-env.ps1 first to generate AUTH_ENCRYPTION_KEY and ADMIN_PASSWORD."
}
$envText = Get-Content ".env" -Raw
foreach ($key in @("AUTH_ENCRYPTION_KEY", "ADMIN_PASSWORD")) {
    if ($envText -notmatch "(?m)^\s*$key\s*=\s*\S") {
        throw ".env is missing a value for $key. Re-run ./setup-env.ps1."
    }
}

if (-not (Test-Path $HostOllama)) {
    throw "Host Ollama store not found at '$HostOllama'. Pass -HostOllama <path>."
}

Write-Host "  Docker OK, .env OK, host Ollama store OK." -ForegroundColor DarkGray

# ------------------------------------------------------------------
# 2. Resolve model list (auto-discover all host models by default)
# ------------------------------------------------------------------
Write-Step "Resolving models"

if (-not $Models -or $Models.Count -eq 0) {
    $ollamaCmd = Get-Command ollama -ErrorAction SilentlyContinue
    if (-not $ollamaCmd) {
        throw "Host 'ollama' CLI not found and no -Models specified. Install Ollama or pass -Models."
    }
    $Models = (& ollama list) |
        Select-Object -Skip 1 |
        ForEach-Object { ($_ -split '\s+')[0] -replace ':latest$', '' } |
        Where-Object { $_ }
}

if (-not $Models -or $Models.Count -eq 0) {
    throw "No models to sync. Pull at least one model on the host (ollama pull <model>) or pass -Models."
}
Write-Host "  Models: $($Models -join ', ')" -ForegroundColor DarkGray

# ------------------------------------------------------------------
# 3. Build router image (clean rebuild lands the array-content fix)
# ------------------------------------------------------------------
if (-not $SkipBuild) {
    Write-Step "Building router image"
    $buildArgs = @("compose", "build")
    if ($NoCache) { $buildArgs += "--no-cache" }
    $buildArgs += "local-model-router"
    & docker @buildArgs
    if (-not $?) { throw "Router image build failed." }
} else {
    Write-Step "Skipping router build (-SkipBuild)"
}

# ------------------------------------------------------------------
# 4. Sync models into the container's Ollama volume
# ------------------------------------------------------------------
Write-Step "Syncing models into container volume"
& "$PSScriptRoot/scripts/sync-models.ps1" -Models $Models -HostOllama $HostOllama
if (-not $?) { throw "Model sync failed." }

# ------------------------------------------------------------------
# 5. Start the stack and refresh Ollama
# ------------------------------------------------------------------
Write-Step "Starting stack"
docker compose up -d
if (-not $?) { throw "docker compose up failed." }

# Restart Ollama so it rescans the volume for the freshly synced models.
docker compose restart ollama | Out-Null

# ------------------------------------------------------------------
# 6. Verify
# ------------------------------------------------------------------
Write-Step "Verifying"

# Wait for the router healthcheck to report healthy (up to ~60s).
$healthy = $false
for ($i = 0; $i -lt 30; $i++) {
    $status = (docker inspect local-model-router --format "{{.State.Health.Status}}" 2>$null)
    if ($status -eq "healthy") { $healthy = $true; break }
    Start-Sleep -Seconds 2
}

Write-Host "`nContainer Ollama models:" -ForegroundColor Green
docker exec ollama ollama list

Write-Host "`nRouter health: " -NoNewline -ForegroundColor Green
if ($healthy) {
    Write-Host "healthy" -ForegroundColor Green
} else {
    Write-Host "not healthy yet (check 'docker compose logs local-model-router')" -ForegroundColor Yellow
}

Write-Host "`nAdmin UI : http://localhost:8080/admin" -ForegroundColor Cyan
Write-Host "API base : http://localhost:8080/v1  (requires an API key)" -ForegroundColor Cyan
Write-Host "Done." -ForegroundColor Green
