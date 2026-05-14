# syntax=docker/dockerfile:1

ARG GO_VERSION=1.26.2
ARG HUGO_VERSION=0.151.0

# ---- build stage -----------------------------------------------------------
FROM golang:${GO_VERSION}-bookworm AS builder

WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Build the CLI. The main package lives in ./cmd/ftg (module
# github.com/sandgardenhq/find-the-gaps), not the repo root — see the Makefile's
# `build` target. CGO must stay enabled: the language scanners link against
# go-tree-sitter (C). The golang:*-bookworm image already ships a C toolchain.
COPY . .
RUN go build -v -trimpath -o /out/ftg ./cmd/ftg

# ---- runtime stage ---------------------------------------------------------
# node base gives us node + npm so we can install `mdfetch`; we also pull the
# Hugo "extended" binary. Both are runtime dependencies of `ftg analyze`.
FROM node:22-bookworm-slim AS runtime

ARG HUGO_VERSION

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl git unzip \
 && rm -rf /var/lib/apt/lists/*

# Hugo (extended) — used to render the report site.
RUN arch="$(dpkg --print-architecture)" \
 && case "$arch" in \
      amd64) hugo_arch=amd64 ;; \
      arm64) hugo_arch=arm64 ;; \
      *) echo "unsupported arch: $arch" >&2; exit 1 ;; \
    esac \
 && curl -fsSL "https://github.com/gohugoio/hugo/releases/download/v${HUGO_VERSION}/hugo_extended_${HUGO_VERSION}_linux-${hugo_arch}.tar.gz" \
      | tar -xz -C /usr/local/bin hugo \
 && hugo version

# mdfetch — used to ingest documentation sites.
RUN npm install -g @sandgarden/mdfetch@latest \
 && mdfetch --version

# AWS CLI v2 — used by the entrypoint script to upload the report tarball to
# Fly Storage (Tigris is S3-compatible) and to generate the presigned URL.
RUN arch="$(dpkg --print-architecture)" \
 && case "$arch" in \
      amd64) aws_arch=x86_64 ;; \
      arm64) aws_arch=aarch64 ;; \
      *) echo "unsupported arch: $arch" >&2; exit 1 ;; \
    esac \
 && curl -fsSL "https://awscli.amazonaws.com/awscli-exe-linux-${aws_arch}.zip" -o /tmp/awscliv2.zip \
 && unzip -q /tmp/awscliv2.zip -d /tmp \
 && /tmp/aws/install \
 && rm -rf /tmp/awscliv2.zip /tmp/aws \
 && aws --version

COPY --from=builder /out/ftg /usr/local/bin/ftg

# Entrypoint script that drives a single analyze + upload job.
COPY deploy/fly/run-job.sh /usr/local/bin/run-job
RUN chmod +x /usr/local/bin/run-job

# Invoked as a one-shot job via `fly machine run`. Two positional args are
# required: <repo-url> <docs-url>. The presigned download URL is the last line
# of stdout. Note: `.find-the-gaps/` is excluded by .dockerignore, so any
# cached project state must be mounted via a Fly volume.
ENTRYPOINT ["/usr/local/bin/run-job"]
