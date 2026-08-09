FROM golang:1.23 AS builder

ARG GOOS linux
ARG GOARCH amd64
ARG WORKER_APP image

WORKDIR /app

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    libvips-dev \
    libjpeg-dev \
    libjpeg62-turbo-dev \
    libwebp-dev \
    libpng-dev \
    libtiff-dev \
    libexif-dev \
    libc6 \
    libx264-dev \
    libx265-dev \
    libvpx-dev \
    libaom-dev \
    libsvtav1-dev

COPY go.mod go.mod
COPY go.sum go.sum
COPY default.pgo default.pgo
COPY src src
COPY cmd cmd

# Build the appropriate worker based on WORKER_APP arg
RUN if [ "$WORKER_APP" = "video" ]; then \
        GOOS=$GOOS GOARCH=$GOARCH go build -ldflags="-s -w" -o /tmp/energon ./cmd/video-worker/main.go; \
    else \
        GOOS=$GOOS GOARCH=$GOARCH go build -ldflags="-s -w" -o /tmp/energon ./cmd/worker/main.go; \
    fi

FROM alpine:edge

RUN apk add --no-cache \
    vips \
    libjpeg-turbo \
    libwebp \
    tiff \
    musl \
    gcompat \
    curl \
    libc6-compat \
    nginx \
    supervisor \
    ffmpeg

WORKDIR /app

COPY --from=builder /tmp/energon /app/worker/energon

COPY ./config /app/config

ENV CGO_CFLAGS_ALLOW="-Xpreprocessor"
ENV AWS_REGION=us-east-1

RUN chmod +x /app/worker/energon && mkdir -p /tmp/shm/image_cache

CMD ["/app/worker/energon"]
