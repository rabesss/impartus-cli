# syntax=docker/dockerfile:1.7

# golang:1.25-bookworm digest last updated: 2026-05-17
# To update: docker pull golang:1.25-bookworm && replace digest below
FROM golang:1.26-bookworm@sha256:252599aeb51ad60b83e4d8821802068127c528c707cb7dd7afd93be057c6011c AS build

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

# debian:bookworm-slim digest last updated: 2026-05-25
# To update: docker pull debian:bookworm-slim && replace digest below
FROM debian:bookworm-slim@sha256:0104b334637a5f19aa9c983a91b54c89887c0984081f2068983107a6f6c21eeb

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

RUN useradd --create-home --shell /usr/sbin/nologin impartus

WORKDIR /work

COPY --from=build /out/impartus /usr/local/bin/impartus

EXPOSE 8080

USER impartus

ENTRYPOINT ["impartus"]
CMD ["--help"]
