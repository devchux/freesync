GO ?= go
BUILD_DIR ?= bin
PREFIX ?= $(HOME)/.local
INSTALL_DIR ?= $(PREFIX)/bin
CLI = $(BUILD_DIR)/freesync
MACOS_CLI = $(BUILD_DIR)/freesync-macos

.DEFAULT_GOAL := help
.PHONY: help check-go check-macos deps deps-macos build build-macos install install-macos setup list mount

help:
	@printf '%s\n' \
		'make deps           Download Go module dependencies' \
		'make deps-macos     Install macOS prerequisites with Homebrew' \
		'make build          Build the CLI in bin/' \
		'make build-macos    Build the CLI with macFUSE support' \
		'make install        Install the CLI to ~/.local/bin' \
		'make install-macos  Install the mount-enabled CLI to ~/.local/bin' \
		'make setup          Connect a storage provider' \
		'make list           List objects from the saved connection' \
		'make mount          Mount the saved connection read-only on macOS'

check-go:
	@command -v "$(GO)" >/dev/null 2>&1 || { \
		echo 'Install Go 1.27.4 or later from https://go.dev/dl/.' >&2; exit 1; \
	}

check-macos:
	@test "$$(uname -s)" = Darwin || { echo 'This target requires macOS.' >&2; exit 1; }

deps: check-go
	"$(GO)" mod download

deps-macos: check-macos
	@xcode-select -p >/dev/null 2>&1 || { \
		echo 'Run xcode-select --install, finish the installer, then rerun make deps-macos.' >&2; exit 1; \
	}
	@command -v brew >/dev/null 2>&1 || { \
		echo 'Install Homebrew from https://brew.sh/ or follow the manual steps in README.md.' >&2; exit 1; \
	}
	@command -v "$(GO)" >/dev/null 2>&1 || brew install go
	@test -d /Library/Filesystems/macfuse.fs || brew install --cask macfuse
	"$(GO)" mod download

build: check-go
	mkdir -p "$(BUILD_DIR)"
	"$(GO)" build -o "$(CLI)" ./cmd/freesync

build-macos: check-macos check-go
	mkdir -p "$(BUILD_DIR)"
	CGO_ENABLED=1 "$(GO)" build -tags macfuse -o "$(MACOS_CLI)" ./cmd/freesync

install: build
	mkdir -p "$(INSTALL_DIR)"
	install -m 0755 "$(CLI)" "$(INSTALL_DIR)/freesync"

install-macos: build-macos
	mkdir -p "$(INSTALL_DIR)"
	install -m 0755 "$(MACOS_CLI)" "$(INSTALL_DIR)/freesync"

setup: build
	"$(CLI)" storage setup

list: build
	"$(CLI)" storage list

mount: build-macos
	"$(MACOS_CLI)" storage mount
