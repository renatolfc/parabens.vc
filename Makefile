BINARY_NAME ?= parabens-vc
BIN_DIR ?= bin

.PHONY: build build-arm64 build-amd64 clean

default: build-arm64 build-amd64

build:
	@mkdir -p $(BIN_DIR)
	GOOS=$(shell go env GOOS) GOARCH=$(shell go env GOARCH) CGO_ENABLED=0 \
		go build -trimpath -ldflags "-s -w" -o $(BIN_DIR)/$(BINARY_NAME) .

build-arm64:
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
		go build -trimpath -ldflags "-s -w" -o $(BIN_DIR)/$(BINARY_NAME)-arm64 .

build-amd64:
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
		go build -trimpath -ldflags "-s -w" -o $(BIN_DIR)/$(BINARY_NAME)-amd64 .

clean:
	rm -rf $(BIN_DIR)
