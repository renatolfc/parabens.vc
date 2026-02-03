BINARY_NAME ?= parabens-vc
BIN_DIR ?= bin

.PHONY: build build-arm64 build-amd64 install-user-service clean

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

install-user-service: build
	@install -d "$(HOME)/parabens.vc" "$(HOME)/parabens.vc/data" "$(HOME)/.cache/parabens.vc" "$(HOME)/.config/systemd/user"
	@install -m 0755 "$(BIN_DIR)/$(BINARY_NAME)" "$(HOME)/parabens.vc/$(BINARY_NAME)"
	@if [ ! -f "$(HOME)/parabens.vc/.env" ]; then \
		printf '%s\n' \
			'PORT=8080' \
			'PUBLIC_BASE_URL=https://parabens.vc' \
			'SHORTLINK_DB=%h/parabens.vc/data/shortlinks.json' \
			'XDG_CACHE_DIR=%h/.cache' \
			> "$(HOME)/parabens.vc/.env"; \
		echo "Created $(HOME)/parabens.vc/.env"; \
	fi
	@install -m 0644 deploy/parabens-vc.user.service "$(HOME)/.config/systemd/user/parabens-vc.service"
	@systemctl --user daemon-reload
	@systemctl --user enable --now parabens-vc
