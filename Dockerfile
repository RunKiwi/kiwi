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
# while dropping the ~300MB Go toolchain from the shipped image.
FROM alpine:3.20

WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata git

COPY --from=builder /out/kiwid /out/kiwidaemon ./

EXPOSE 8080
ENTRYPOINT ["./kiwid"]
