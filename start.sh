#!/usr/bin/env bash
# Linux bootstrap: prepares config/data, auto-enables NVIDIA GPU if usable, starts the stack.
set -euo pipefail
cd "$(dirname "$0")"

[ -f .env ] && { set -a; . ./.env; set +a; }
: "${AUTH_ENCRYPTION_KEY:?not set - run 'source setup-env.sh' (and save to .env)}"
: "${ADMIN_PASSWORD:?not set - run 'source setup-env.sh' (and save to .env)}"

# A missing bind-mount source makes Docker create a directory; undo that.
[ -d config.yaml ] && rmdir config.yaml
[ -f config.yaml ] || cp config.example.yaml config.yaml
# Container runs as non-root; Docker would otherwise create ./data as root.
mkdir -p data && chmod a+rwx data

files=(-f docker-compose.yml)
if command -v nvidia-smi >/dev/null && nvidia-smi -L >/dev/null 2>&1 \
   && docker info --format '{{json .Runtimes}}' | grep -q nvidia; then
  echo "NVIDIA GPU + container runtime detected: enabling GPU"
  files+=(-f docker-compose.gpu.yml)
else
  echo "No usable NVIDIA GPU/runtime: running Ollama on CPU"
fi

docker compose "${files[@]}" up -d --build
