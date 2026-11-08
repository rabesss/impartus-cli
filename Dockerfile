# syntax=docker/dockerfile:1.7

# The golang tag must match the Go version in go.mod (the `go` line, or a
# `toolchain` line if one is added); container_build_test.go enforces it. Bump
# both, plus the digest, in one change.
# The build stage runs on the build host and cross-compiles for each target
# platform, so arm64 images do not compile Go under QEMU.
FROM --platform=$BUILDPLATFORM golang:1.27.3-bookworm@sha256:dc0963f32053e4bc451d24d87e3892d42d01021b61f093e98eb95c0352bb84b0 AS build

ARG VERSION=dev
ARG BUILD_DATE=""

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Declared after go mod download so that layer is shared by every target platform.
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
	go build -trimpath \
	-ldflags "-s -w \
	  -X github.com/rabesss/impartus-cli/internal/buildinfo.Version=${VERSION} \
	  -X github.com/rabesss/impartus-cli/internal/buildinfo.Date=${BUILD_DATE}" \
	-o /out/impartus .

# Dated tag plus digest so Dependabot sees each Debian point refresh as a new
# version. A rolling bookworm-slim tag let one closed digest PR suppress every
# later update (#304).
FROM debian:bookworm-20261104-slim@sha256:734a72b3d6142a71993b4bc580ca01d8ca687f77a2c422d96a961d3376b179bc

ARG VERSION=dev
ARG BUILD_DATE=""
ARG COMMIT=unknown

LABEL org.opencontainers.image.title="impartus-cli"
LABEL org.opencontainers.image.description="CLI and HTTP API for downloading Impartus lecture recordings."
LABEL org.opencontainers.image.url="https://github.com/rabesss/impartus-cli"
LABEL org.opencontainers.image.source="https://github.com/rabesss/impartus-cli"
LABEL org.opencontainers.image.licenses="MIT"
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.created="${BUILD_DATE}"
LABEL org.opencontainers.image.revision="${COMMIT}"

RUN apt-get update && \
    apt-get install --no-install-recommends -y \
      ca-certificates ffmpeg liblzma5 libpcre2-8-0 perl-base tzdata && \
    rm -rf /var/lib/apt/lists/*

RUN useradd --create-home --shell /usr/sbin/nologin impartus && \
    install -d -o impartus -g impartus -m 0750 \
      /work /work/temp /work/downloads

WORKDIR /work

COPY --from=build /out/impartus /usr/local/bin/impartus

EXPOSE 8080

USER impartus

ENTRYPOINT ["impartus"]
CMD ["--help"]
