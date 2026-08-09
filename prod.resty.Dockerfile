FROM golang:1.23 AS builder

ARG GOOS linux
ARG GOARCH amd64
ARG BUILD_ARCH $GOOS
ARG BUILD_OS $GOARCH

WORKDIR /app

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    libvips-dev \
    libjpeg-dev \
    libwebp-dev \
    libpng-dev \
    libtiff-dev \
    libexif-dev \
    libc6


COPY go.mod go.mod
COPY go.sum go.sum
COPY default.pgo default.pgo
COPY src src
COPY cmd cmd

RUN GOOS=$GOOS GOARCH=$GOARCH go build -ldflags="-s -w" -o /tmp/optimux ./cmd/server/main.go

FROM alpine:edge

ENV LUAROCKS_VERSION=3.11.1

RUN set -ex \
    \
    && apk add --no-cache \
        ca-certificates \
        openssl \
        wget \
        lua5.1 \
    \
    && apk add --no-cache --virtual .build-deps \
        make \
        gcc \
        libc-dev \
        lua5.1-dev \
    \
    && wget https://luarocks.github.io/luarocks/releases/luarocks-${LUAROCKS_VERSION}.tar.gz \
        -O - | tar -xzf - \
    \
    && cd luarocks-${LUAROCKS_VERSION} \
    && ./configure --lua-version=5.1 \
            --with-lua=/usr \
            --with-lua-bin=/usr/bin \
            --with-lua-include=/usr/include/ \
    \
    && make build \
    && make install \
    && cd .. \
    && rm -rf luarocks-${LUAROCKS_VERSION} \
    && luarocks install luasocket \
    \
    && apk del .build-deps

RUN apk add --no-cache \
    vips \
    libjpeg-turbo \
    libwebp \
    tiff \
    musl \
    gcompat \
    curl \
    libc6-compat \
    supervisor \
    openresty \
    ffmpeg

WORKDIR /app

COPY --from=builder /tmp/optimux /app/optimux/server

COPY ./config /app/config
COPY ./infra/prod.resty.conf /etc/nginx/nginx.conf
COPY ./infra/lua/*.lua /usr/local/share/lua/5.1/
COPY ./infra/supervisord.conf /etc/supervisor/conf.d/supervisord.conf
COPY ./infra/entrypoint.sh /app/optimux/entrypoint.sh

ENV CGO_CFLAGS_ALLOW="-Xpreprocessor"
ENV S3_BASE_URL=
ENV TMPDIR=/tmp/shm
ENV ORIGINS=

CMD ["sh", "/app/optimux/entrypoint.sh"]

# CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
# CMD ["/app/optimux/server"]
