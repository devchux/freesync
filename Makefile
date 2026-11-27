GO ?= go
BUILD_DIR ?= bin
PREFIX ?= $(HOME)/.local
INSTALL_DIR ?= $(PREFIX)/bin
CLI = $(BUILD_DIR)/freesync
MACOS_CLI = $(BUILD_DIR)/freesync-macos
LINUX_CLI = $(BUILD_DIR)/freesync-linux
WINDOWS_ARCH ?= amd64
WINDOWS_CLI = $(BUILD_DIR)/freesync-windows-$(WINDOWS_ARCH).exe
HOST_OS := $(shell uname -s)

.DEFAULT_GOAL := help
.PHONY: help check-go check-macos check-linux check-linux-fuse deps deps-macos deps-linux build build-macos \
	build-linux build-windows install install-macos install-linux setup list mount mount-write

help:
	@printf '%s\n' \
		'make deps           Download Go module dependencies' \
		'make deps-macos     Install macOS prerequisites with Homebrew' \
		'make deps-linux     Check Linux FUSE 3 headers and download Go modules' \
		'make build          Build the CLI in bin/' \
		'make build-macos    Build the CLI with macFUSE support' \
		'make build-linux    Build the CLI with FUSE 3 support' \
		'make build-windows  Build a Windows executable with WinFsp support' \
		'make install        Install the CLI to ~/.local/bin' \
		'make install-macos  Install the macOS mount-enabled CLI to ~/.local/bin' \
		'make install-linux  Install the Linux mount-enabled CLI to ~/.local/bin' \
		'make setup          Connect a storage provider' \
		'make list           List objects from the saved connection' \
		'make mount          Mount the saved connection read-only on macOS or Linux' \
		'make mount-write    Mount the saved S3 or Azure Blob connection with write access'

check-go:
	@command -v "$(GO)" >/dev/null 2>&1 || { \
		echo 'Install Go 1.27.4 or later from https://go.dev/dl/.' >&2; exit 1; \
	}

check-macos:
	@test "$$(uname -s)" = Darwin || { echo 'This target requires macOS.' >&2; exit 1; }

check-linux:
	@test "$$(uname -s)" = Linux || { echo 'This target requires Linux.' >&2; exit 1; }

check-linux-fuse: check-linux
	@pkg-config --exists fuse3 || { \
		echo 'Install the FUSE 3 development headers and pkg-config. See README.md.' >&2; exit 1; \
	}

deps-linux: check-linux-fuse check-go
	"$(GO)" mod download

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

build-linux: check-linux-fuse check-go
	mkdir -p "$(BUILD_DIR)"
	CGO_ENABLED=1 CGO_CPPFLAGS="$$CGO_CPPFLAGS $$(pkg-config --cflags fuse3)" "$(GO)" build -tags fuse3 -o "$(LINUX_CLI)" ./cmd/freesync

build-windows: check-go
	mkdir -p "$(BUILD_DIR)"
	GOOS=windows GOARCH="$(WINDOWS_ARCH)" CGO_ENABLED=0 "$(GO)" build -tags winfsp -o "$(WINDOWS_CLI)" ./cmd/freesync

install: build
	mkdir -p "$(INSTALL_DIR)"
	install -m 0755 "$(CLI)" "$(INSTALL_DIR)/freesync"

install-macos: build-macos
	mkdir -p "$(INSTALL_DIR)"
	install -m 0755 "$(MACOS_CLI)" "$(INSTALL_DIR)/freesync"

install-linux: build-linux
	mkdir -p "$(INSTALL_DIR)"
	install -m 0755 "$(LINUX_CLI)" "$(INSTALL_DIR)/freesync"

setup: build
	"$(CLI)" storage setup

list: build
	"$(CLI)" storage list

ifeq ($(HOST_OS),Linux)
mount: build-linux
	"$(LINUX_CLI)" storage mount

mount-write: build-linux
	"$(LINUX_CLI)" storage mount --write
else ifeq ($(HOST_OS),Darwin)
mount: build-macos
	"$(MACOS_CLI)" storage mount

mount-write: build-macos
	"$(MACOS_CLI)" storage mount --write
else
mount mount-write:
	@echo 'See README.md for platform-specific mount instructions.' >&2; exit 1
endif
