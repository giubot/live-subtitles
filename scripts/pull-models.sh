#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Download the default local models:
#   - whisper.cpp GGML model into ./models (verified against Hugging Face's SHA-256)
#   - Gemma via Ollama (native `ollama` if it's running, else the compose container)
# Override with WHISPER_MODEL (large-v3-turbo | medium | small; multilingual
# only, never *.en) and GEMMA_MODEL (default gemma3:4b; gemma3:1b is the
# small fallback). SKIP_WHISPER=1 / SKIP_GEMMA=1 skip one.
set -euo pipefail

cd "$(dirname "$0")/.."
whisper_model=${WHISPER_MODEL:-large-v3-turbo}
gemma_model=${GEMMA_MODEL:-gemma3:4b}

case "$whisper_model" in
  *.en) echo "WHISPER_MODEL=$whisper_model is English-only; Spanish needs a multilingual model" >&2; exit 1 ;;
esac

if [ -z "${SKIP_WHISPER:-}" ]; then
  file="models/ggml-$whisper_model.bin"
  url="https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-$whisper_model.bin"
  want=$(curl -fsSIL "$url" | tr -d '\r' | awk -F': ' 'tolower($1)=="x-linked-etag" {gsub(/"/,"",$2); print $2}' | tail -1)
  [ -n "$want" ] || { echo "no checksum for $url; is the model name right?" >&2; exit 1; }
  mkdir -p models
  if [ -f "$file" ] && [ "$(shasum -a 256 "$file" | cut -d' ' -f1)" = "$want" ]; then
    echo "$file is up to date"
  else
    echo "downloading $file"
    curl -fL -C - --progress-bar -o "$file" "$url"
    got=$(shasum -a 256 "$file" | cut -d' ' -f1)
    [ "$got" = "$want" ] || { echo "$file: sha256 $got, want $want" >&2; exit 1; }
    echo "$file verified"
  fi
fi

if [ -z "${SKIP_GEMMA:-}" ]; then
  if command -v ollama >/dev/null && ollama list >/dev/null 2>&1; then
    ollama pull "$gemma_model"
  else
    docker compose -f deploy/compose/compose.dev.yaml exec ollama ollama pull "$gemma_model"
  fi
fi
