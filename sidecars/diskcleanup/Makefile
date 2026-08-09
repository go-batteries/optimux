APP_VERSION ?= latest
BUILD_OS ?= linux
BUILD_ARCH ?= amd64

build:
	docker build --platform=$(BUILD_OS)/$(BUILD_ARCH) -t optimux/optimux-cleaner:$(APP_VERSION) -f Dockerfile .

push:
	docker push optimux/optimux-cleaner:$(APP_VERSION)
