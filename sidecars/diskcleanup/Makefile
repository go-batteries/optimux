APP_VERSION ?= latest
BUILD_OS ?= linux
BUILD_ARCH ?= amd64
DOCKER_ORG ?= optimux

build:
	docker build --platform=$(BUILD_OS)/$(BUILD_ARCH) -t $(DOCKER_ORG)/optimux-cleaner:$(APP_VERSION) -f Dockerfile .

push:
	docker push $(DOCKER_ORG)/optimux-cleaner:$(APP_VERSION)
