# syntax=docker/dockerfile:1.7
# Builds the kiwid (Control Plane) and kiwidaemon binaries.
#
# The builder runs on the *native* build platform and cross-compiles to the
# target arch (Go does this natively), so building a linux/amd64 image on an
# arm64 Mac never emulates the compiler under QEMU. Go's module + build caches
# are persisted via BuildKit cache mounts, so incremental builds are fast.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

WORKDIR /app
RUN apk add --no-cache git

# Download modules first so this layer is cached until go.mod/go.sum change.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# TARGETOS/TARGETARCH are provided by buildx from --platform.
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/kiwid ./cmd/kiwid && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/kiwidaemon ./cmd/kiwidaemon

# Minimal runtime — alpine keeps git available (gitcache shells out to it)
# while dropping the ~300MB Go toolchain from the shipped image. Both binaries
# ship in the base layer; the two final stages differ only by entrypoint.
FROM alpine:3.20 AS base

WORKDIR /app
# docker-cli: the provisioner (kiwid) and the sandbox (kiwidaemon) shell out to
# `docker` — the provisioner to launch per-org daemon containers, the daemon to
# run the test command as a sibling container under runsc. Both talk to the host
# Docker daemon via a bind-mounted /var/run/docker.sock.
RUN apk add --no-cache ca-certificates tzdata git docker-cli

COPY --from=builder /out/kiwid /out/kiwidaemon ./

# Data Plane daemon image — build with `--target kiwidaemon`. Used by the
# free-tier provisioner, which runs `docker run kiwidaemon:latest`.
FROM base AS kiwidaemon
ENTRYPOINT ["./kiwidaemon"]

# Control Plane image — the default target (last stage), so an untargeted
# `docker build` still produces the kiwid image as before.
FROM base AS kiwid

EXPOSE 8080
ENTRYPOINT ["./kiwid"]
