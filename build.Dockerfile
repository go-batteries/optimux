FROM golang:1.23 AS builder

WORKDIR /app

ARG GOOS linux
ARG GOARCH amd64

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    libvips-dev \
    libjpeg-dev \
    libwebp-dev \
    libpng-dev \
    libtiff-dev \
    libexif-dev


COPY go.mod go.mod
COPY go.sum go.sum
COPY default.pgo default.pgo
COPY tsunami tsunami

RUN GOOS=$GOOS GOARCH=$GOARCH go build -ldflags="-s -w" -o /tmp/optimux ./tsunami/main.go

FROM alpine:edge AS compressor

RUN apk add --no-cache \
    vips \
    libjpeg-turbo \
    libwebp \
    tiff \
    musl \
    gcompat \
    curl \
    xz-dev \
    upx


COPY --from=builder /tmp/optimux /tmp/optimux

RUN upx --best --lzma /tmp/optimux

FROM alpine:edge

COPY --from=compressor /tmp/optimux /output/optimux
