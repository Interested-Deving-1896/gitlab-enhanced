BINARY    := garm-provider-gitlab
BUILD_DIR := bin
GO        := go
LDFLAGS   := -s -w

.PHONY: all build install clean test

all: build

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) .

install: build
	install -Dm755 $(BUILD_DIR)/$(BINARY) /usr/local/bin/$(BINARY)
	@echo "Installed /usr/local/bin/$(BINARY)"
	@echo "Next: configure /etc/garm/providers.d/gitlab.toml"
	@echo "      See README.md for full setup instructions."

test:
	$(GO) test ./...

clean:
	rm -rf $(BUILD_DIR)
