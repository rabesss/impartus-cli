# syntax=docker/dockerfile:1.7

# golang:1.26.5-bookworm digest last updated: 2026-07-13
# To update: docker pull golang:1.26.5-bookworm && replace digest below
FROM golang:1.26.5-bookworm@sha256:18aedc16aa19b3fd7ded7245fc14b109e054d65d22ed53c355c899582bbb2113 AS build

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev
ARG BUILD_DATE=""

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
	go build -trimpath \
	-ldflags "-s -w \
	  -X github.com/rabesss/impartus-cli/internal/buildinfo.Version=${VERSION} \
	  -X github.com/rabesss/impartus-cli/internal/buildinfo.Date=${BUILD_DATE}" \
	-o /out/impartus .

# debian:bookworm-slim digest last updated: 2026-06-30
# To update: docker pull debian:bookworm-slim && replace digest below
FROM debian:bookworm-slim@sha256:60eac759739651111db372c07be67863818726f754804b8707c90979bda511df

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
    apt-get install --no-install-recommends -y ca-certificates ffmpeg && \
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
