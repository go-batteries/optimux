GOOS ?= linux
GOARCH ?= amd64
BUILD_ARCH ?= $(shell uname -m | sed 's/x86_64/amd64/')
BUILD_OS ?= $(shell uname -s | tr '[:upper:]' '[:lower:]')
APP_VERSION ?= latest
ORIGINS ?= "*"
ENV ?= dev

tmpfs.setup:
	mkdir -p /tmp/shm/image_cache
	sudo mount -t tmpfs -o size=1G tmpfs /tmp/shm/image_cache

gen.certs:
	openssl req -x509 -newkey rsa:4096 -keyout server.key -out server.crt -days 365 -nodes

build:
	GOOS=$(BUILD_OS) GOARCH=$(BULID_ARCH) CGO_ENABLED=1 go build -race -o out/$(BUILD_ARCH)/optimux cmd/server/main.go

run: build
	CGO_CFLAGS_ALLOW="-Xpreprocessor" ENV=$(ENV) S3_BASE_URL=$(S3_BASE_URL) ORIGINS=$(ORIGINS) ./out/$(BUILD_ARCH)/optimux

build.worker:
	GOOS=$(GOOS) GOARCH=$(BUILD_ARCH) CGO_ENABLED=1 go build -ldflags="-s -w -extldflags '-static'" -tags lambda.norpc -o out/$(BUILD_ARCH)/bootstrap cmd/worker/main.go
	zip -j ./infra/terraform/energon/lambda.zip out/$(BUILD_ARCH)/bootstrap

build.linux:
	CGO_LDFLAGS="-static" \
				CGO_ENABLED=1  \
				CC=aarch64-alpine-linux-musl-gcc \
				GOOS=linux \
				GOARCH=arm64 \
				go build -o bootstrap cmd/worker/main.go

	zip -j lambda.zip bootstrap

# build.single:
# 	go build -race -o out/optimux_1 simple/main.go
# 	GOOS=$(BUILD_OS) GOARCH=$(BUILD_ARCH) go build -race -o out/$(BUILD_OS)/optimux_1 simple/main.go
#
# build.docker.linux:
# 	docker build $(NO_CACHE) --platform linux/$(GOARCH) -t optimux-builder-compressed -f build.Dockerfile .
# 	docker run --platform linux/$(GOARCH) --name optimux-linux-builder optimux-builder-compressed
# 	docker cp optimux-linux-builder:/output/optimux ./out/linux/
# 	docker rm optimux-linux-builder

build.docker.app:
	docker build \
		--platform=$(GOOS)/$(GOARCH) $(NO_CACHE) \
		--progress=plain \
		--build-arg GOOS=$(GOOS) \
		--build-arg GOARCH=$(GOARCH) \
		--build-arg BUILD_ARCH=$(BUILD_ARCH) \
		--build-arg BUILD_OS=$(BUILD_OS) $(BUILD_OPTS) \
		-t optimux/optimux-server:$(APP_VERSION) -f Dockerfile .

# run.batch:
# 	CGO_CFLAGS_ALLOW="-Xpreprocessor" go run -race parallels/main.go
#
# run.single:
	# CGO_CFLAGS_ALLOW="-Xpreprocessor" ./out/optimux_1
run.docker.app:
	docker run --rm \
		--memory 4G \
		# --platform=$(GOOS)/$(GOARCH) \
		-e S3_BASE_URL=$(S3_BASE_URL) \
		-e ORIGINS=$(ORIGINS) \
		--tmpfs /tmp/shm/image_cache:rw,size=2G \
		-v ./tmp/shm/edge_cache:/tmp/shm/edge_cache \
		-v ./tmp/log/:/var/log \
		-p 8811:8811 -p 9090:80 \
		optimux/optimux-server:$(APP_VERSION)


push.docker.app: build.docker.app
	docker push optimux/optimux-server:$(APP_VERSION)
	docker image rm optimux/optimux-server:$(APP_VERSION)


build.docker.worker:
	docker buildx build --platform linux/amd64 --provenance=false -t energon-worker:$(APP_VERSION) -f worker.Dockerfile . $(BUILD_OPTS)

push.docker.worker: build.docker.worker
	docker tag energon-worker:$(APP_VERSION) $(AWS_ACCOUNT).dkr.ecr.us-east-1.amazonaws.com/energon-worker:$(APP_VERSION)
	docker push $(AWS_ACCOUNT).dkr.ecr.us-east-1.amazonaws.com/energon-worker:$(APP_VERSION)


migration.setup:
	go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest

build.docker.migrate:
	docker buildx build --platform linux/amd64 -t optimux-migrator:$(APP_VERSION) -f migration.Dockerfile . $(BUILD_OPTS)

push.docker.migrate: build.docker.migrate
	docker tag optimux-migrator:$(APP_VERSION) $(AWS_ACCOUNT).dkr.ecr.us-east-1.amazonaws.com/optimux-migrator:$(APP_VERSION)
	docker push $(AWS_ACCOUNT).dkr.ecr.us-east-1.amazonaws.com/optimux-migrator:$(APP_VERSION)

