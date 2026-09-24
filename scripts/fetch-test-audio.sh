#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Download one English and one Spanish Nerdearla talk and trim them to
# 16 kHz mono AAC clips in testdata/audio/ (gitignored), for the file
# source (`task demo:file`) and latency/accuracy checks.
#
# Needs yt-dlp and ffmpeg. Override the talks or the clip window with env:
#   EN_URL, ES_URL, START (seconds, default 120), DURATION (seconds, default 600)
set -euo pipefail

cd "$(dirname "$0")/.."
out=testdata/audio
start=${START:-120}
duration=${DURATION:-600}

# "Model Context Protocol in Plain English" – Nate Barbettini (spoken: en-US)
en_url=${EN_URL:-https://www.youtube.com/watch?v=iaG9pHMJ3Y4}
# "Python + Machine Learning: de los fundamentos al primer modelo predictivo" – Pablo Marino (spoken: es)
es_url=${ES_URL:-https://www.youtube.com/watch?v=SdWodxGEAB4}

for tool in yt-dlp ffmpeg; do
  command -v "$tool" >/dev/null || { echo "$tool is required (brew install $tool)" >&2; exit 1; }
done

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fetch() { # lang url
  local lang=$1 url=$2
  echo "fetching $lang: $url"
  yt-dlp --quiet --no-warnings --no-playlist -f bestaudio -o "$tmp/$lang.%(ext)s" "$url"
  ffmpeg -loglevel error -y -ss "$start" -t "$duration" -i "$tmp/$lang".* \
    -vn -ac 1 -ar 16000 -c:a aac -b:a 48k "$out/$lang.m4a"
  echo "wrote $out/$lang.m4a (${duration}s from ${start}s)"
}

mkdir -p "$out"
fetch en "$en_url"
fetch es "$es_url"
