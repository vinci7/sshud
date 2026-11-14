# syntax=docker/dockerfile:1
# Build the UI, then a static Go binary that embeds it, then copy that into a
# minimal image running as an unprivileged user.
#
# Multi-arch: both build stages run on the build machine's platform. The UI
# does not depend on the target architecture, so it is built once; Go
# cross-compiles for each target. Example:
#   docker buildx build --platform linux/amd64,linux/arm64,linux/arm/v7 \
#     --build-arg VERSION=v0.1.0 -t sshud .
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS go
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
# TARGETVARIANT is "v7" for linux/arm/v7; GOARM wants the bare number.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /sshud ./cmd/sshud
# a fresh named volume copies this directory's owner, so nonroot can write it
RUN mkdir /data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=go /sshud /sshud
COPY --from=go --chown=65532:65532 /data /data
ENV SSHUD_DATA=/data
VOLUME /data
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/sshud", "--listen", "0.0.0.0:8080"]
