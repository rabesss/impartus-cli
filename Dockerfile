# syntax=docker/dockerfile:1.7

# golang:1.26-bookworm digest last updated: 2026-05-25
# To update: docker pull golang:1.26-bookworm && replace digest below
FROM golang:1.26-bookworm@sha256:5d2b868674b57c9e48cdd39e891acce4196b6926ca6d11e9c270a8f85106203d AS build

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
FROM debian:bookworm-slim@sha256:96e378d7e6531ac9a15ad505478fcc2e69f371b10f5cdf87857c4b8188404716

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
