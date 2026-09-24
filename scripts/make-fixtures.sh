#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Regenerate the tiny committed audio fixtures in testdata/audio/fixtures/
# (16 kHz mono s16le WAV) with macOS text-to-speech. They're synthetic so
# they carry no third-party rights. Needs macOS `say` and ffmpeg.
set -euo pipefail

cd "$(dirname "$0")/.."
out=testdata/audio/fixtures
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

make() { # name voice text
  say -v "$2" -o "$tmp/$1.aiff" "$3"
  ffmpeg -loglevel error -y -i "$tmp/$1.aiff" -ac 1 -ar 16000 -c:a pcm_s16le -map_metadata -1 -fflags +bitexact "$out/$1.wav"
  echo "wrote $out/$1.wav"
}

make en Samantha "Welcome everyone. Today we are going to talk about observability in Kubernetes, and how metrics, logs and traces fit together."
make es Paulina "Bienvenidos a todos. Hoy vamos a hablar de observabilidad en Kubernetes, y de cómo se combinan las métricas, los logs y las trazas."
