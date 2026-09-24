#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Feeds an audio file (or http(s) URL) to a session in real time, through
# POST /api/sessions/{id}/sources/file (AUD-3). The server resolves relative
# paths against its own working directory, which is the repo root in dev.
#
# Usage: scripts/demo-file.sh SESSION FILE [BASE_URL] [LOOP]
# Needs LIVESUBS_ADMIN_TOKEN, set to the same value as the server's.
set -euo pipefail

session=${1:?usage: $0 SESSION FILE [BASE_URL] [LOOP]}
file=${2:?usage: $0 SESSION FILE [BASE_URL] [LOOP]}
base=${3:-http://localhost:8080}
loop=${4:-false}
: "${LIVESUBS_ADMIN_TOKEN:?set LIVESUBS_ADMIN_TOKEN here and in the server environment}"

body=$(python3 -c 'import json, sys; print(json.dumps({"uri": sys.argv[1], "loop": sys.argv[2] == "true"}))' "$file" "$loop")
curl --silent --show-error --fail-with-body \
  -X POST "$base/api/sessions/$session/sources/file" \
  -H "Authorization: Bearer $LIVESUBS_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "$body"
echo
