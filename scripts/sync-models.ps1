# sync-models.ps1
# Copies Ollama models from the host's native store into the fast Docker
# volume used by the containerized Ollama (local_model_router_ollama_models).
#
# Why: bind-mounting the Windows ~/.ollama into the WSL2 container makes model
# loads very slow (drvfs). Copying into a native ext4 Docker volume keeps cold
# loads reasonable. This script copies only the models you name (so you can
# skip huge ones that don't fit your GPU VRAM).
#
# Usage:
#   .\scripts\sync-models.ps1 -Models dolphin3,gemma4
#
param(
    [string[]]$Models = @("dolphin3", "gemma4"),
    [string]$HostOllama = "C:/Users/kalle/.ollama",
    [string]$Volume = "local_model_router_ollama_models"
)

docker volume create $Volume | Out-Null

# Copy each named model's manifest, then every blob it references.
$modelArgs = ($Models -join " ")
$script = @"
set -e
mkdir -p /dst/models/blobs /dst/models/manifests/registry.ollama.ai/library
for m in $modelArgs; do
  cp -r /src/models/manifests/registry.ollama.ai/library/`$m /dst/models/manifests/registry.ollama.ai/library/ || echo "warn: manifest `$m not found"
done
# Copy blobs referenced by the copied manifests.
grep -rho 'sha256:[0-9a-f]*' /dst/models/manifests | sed 's/sha256:/sha256-/' | sort -u | while read b; do
  [ -z "`$b" ] && continue
  if [ ! -f "/dst/models/blobs/`$b" ]; then cp "/src/models/blobs/`$b" "/dst/models/blobs/`$b" && echo "copied `$b"; fi
done
echo "done"
"@

docker run --rm -v "${HostOllama}:/src:ro" -v "${Volume}:/dst" alpine sh -c $script
Write-Host "Restart Ollama to pick up new models: docker compose up -d ollama"
