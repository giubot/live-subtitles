# syntax=docker/dockerfile:1
# SPDX-License-Identifier: Apache-2.0
#
# Live Subtitles server image (P4-02). Build from the repository root:
#   docker build -f deploy/docker/Dockerfile -t livesubs .   (or: task docker:build)
# Stages: web app (Node) → static Go binary with the app embedded → Alpine
# runtime with ffmpeg (built with libsrt, for SRT ingest and recordings).

ARG NODE_VERSION=24
ARG GO_VERSION=1.26
ARG ALPINE_VERSION=3.22

# ── Web app ────────────────────────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-alpine AS web
WORKDIR /src/web
ENV COREPACK_ENABLE_DOWNLOAD_PROMPT=0
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# ── Go binary ──────────────────────────────────────────────────────────────
# Cross-compiled on the build platform: pure Go, so no emulation is needed.
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/
COPY web/embed.go web/
COPY --from=web /src/web/dist/ web/dist/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/livesubs ./cmd/livesubs

# ── Runtime ────────────────────────────────────────────────────────────────
FROM alpine:${ALPINE_VERSION}
# ffmpeg decodes file/URL/SRT sources and encodes recordings. Fail the build
# if this ffmpeg can't do SRT, since SRT ingest (AUD-5) depends on it.
RUN apk add --no-cache ffmpeg ca-certificates tzdata \
    && ffmpeg -hide_banner -protocols 2>/dev/null | grep -qx '  srt' \
    && addgroup -S -g 10001 livesubs \
    && adduser -S -D -H -u 10001 -G livesubs -h /data livesubs \
    && mkdir -p /data && chown livesubs:livesubs /data
COPY --from=build /out/livesubs /usr/local/bin/livesubs
COPY LICENSE NOTICE /usr/share/doc/livesubs/

# There's no OS keychain in a container: secrets go to the encrypted file
# in the data volume. Bind every interface so the published ports work.
ENV LIVESUBS_ADDR=0.0.0.0:8080 \
    LIVESUBS_HTTPS_ADDR=0.0.0.0:8443 \
    LIVESUBS_DATA_DIR=/data \
    LIVESUBS_NO_KEYCHAIN=true
USER livesubs
WORKDIR /data
VOLUME ["/data"]
# HTTP, HTTPS, and UDP for SRT ingest (one port per session from 9000).
EXPOSE 8080/tcp 8443/tcp 9000-9009/udp
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/livesubs"]
