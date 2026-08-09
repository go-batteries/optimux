#!/bin/bash

os=$(uname -s)

if [[ "$os" == "Darwin" ]]; then
	brew install vips pkg-config
	exit 0
fi

if [[ "$os" == "Linux" ]]; then
	sudo apt-get update
	sudo apt-get install -y libvips libvips-dev
	exit 0
fi

echo "Unsupported os $os"
exit 1
