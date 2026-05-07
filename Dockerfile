# syntax=docker/dockerfile:1.7

FROM golang:1.24.7-bookworm AS build

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

FROM debian:bookworm-slim

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
