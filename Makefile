APP_NAME := moxie
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build build-all build-linux build-macos build-windows install clean

# Quick build for current OS
build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o dist/$(APP_NAME) .

build-all: clean build-linux build-macos build-windows

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o dist/$(APP_NAME)-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o dist/$(APP_NAME)-linux-arm64 .

build-macos:
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o dist/$(APP_NAME)-macos-amd64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o dist/$(APP_NAME)-macos-arm64 .

build-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o dist/$(APP_NAME)-windows-amd64.exe .
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o dist/$(APP_NAME)-windows-arm64.exe .

install: build
	cp -f dist/$(APP_NAME) /usr/local/bin/$(APP_NAME)

# ── Desktop (Wails) ──────────────────────────────────────────────
.PHONY: desktop desktop-dev install-desktop

DESKTOP_BINARY := moxie-desktop

# Build desktop application with Wails (requires Wails CLI and webkit2gtk 4.1 on Linux).
# Wails outputs to desktop/build/dist/ — copy to central dist/ after build.
desktop: dist/$(DESKTOP_BINARY)

dist/$(DESKTOP_BINARY): desktop/build/dist/moxie-desktop
	mkdir -p dist
	cp -f desktop/build/dist/moxie-desktop dist/$(DESKTOP_BINARY)
	@echo "  -> dist/$(DESKTOP_BINARY)"

desktop/build/dist/moxie-desktop: desktop/frontend/src/**/* desktop/app.go desktop/main.go
	cd desktop && wails build -tags webkit2_41

# Install desktop application with per-platform app integration
install-desktop: dist/$(DESKTOP_BINARY)
	./scripts/install-desktop.sh

# Run desktop in development mode with hot-reload
desktop-dev:
	cd desktop && wails dev -tags webkit2_41

clean:
	rm -rf dist/
	rm -rf desktop/build/bin/
	rm -rf desktop/frontend/dist/
	rm -rf desktop/frontend/node_modules/
