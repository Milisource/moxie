APP_NAME := moxie
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build build-all build-linux build-macos build-windows install install-cli install-desktop clean

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

# Install CLI + desktop (best-effort). Set MOXIE_SKIP_DESKTOP=1 for CLI only.
install: install-cli install-desktop

# Install CLI to the first writable bin dir on PATH: $$HOME/.local/bin, then
# /usr/local/bin (with sudo fallback). Override with MOXIE_BIN_DIR.
install-cli: build
	@BIN_DIR="$${MOXIE_BIN_DIR:-}"; \
	if [ -z "$$BIN_DIR" ] && [ -d "$$HOME/.local/bin" ] && [ -w "$$HOME/.local/bin" ]; then \
		BIN_DIR="$$HOME/.local/bin"; \
	elif [ -z "$$BIN_DIR" ] && [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then \
		BIN_DIR="/usr/local/bin"; \
	fi; \
	if [ -z "$$BIN_DIR" ]; then \
		sudo cp -f dist/$(APP_NAME) /usr/local/bin/$(APP_NAME) && echo "  -> /usr/local/bin/$(APP_NAME) (via sudo)"; \
	else \
		cp -f dist/$(APP_NAME) "$$BIN_DIR/$(APP_NAME)" && echo "  -> $$BIN_DIR/$(APP_NAME)"; \
	fi

# Desktop install is best-effort: build with wails when available, otherwise
# fall back to any existing prebuilt binary (may be stale), else skip.
install-desktop:
	@if [ "$${MOXIE_SKIP_DESKTOP:-0}" = "1" ]; then \
		echo "==> Skipping desktop install (MOXIE_SKIP_DESKTOP=1)"; \
	elif command -v wails >/dev/null 2>&1; then \
		$(MAKE) dist/$(DESKTOP_BINARY) && ./scripts/install-desktop.sh; \
	elif [ -f desktop/build/dist/moxie-desktop ]; then \
		echo "==> wails CLI not found — installing existing prebuilt desktop binary (may be stale)"; \
		mkdir -p dist && cp -f desktop/build/dist/moxie-desktop dist/$(DESKTOP_BINARY) && ./scripts/install-desktop.sh; \
	else \
		echo "==> Skipping desktop install: wails CLI not found. Install with:"; \
		echo "    go install github.com/wailsapp/wails/v2/cmd/wails@latest"; \
	fi

# ── Desktop (Wails) ──────────────────────────────────────────────
.PHONY: desktop desktop-dev install-desktop

DESKTOP_BINARY := moxie-desktop

# Build desktop application with Wails (requires Wails CLI and webkit2gtk 4.1 on Linux).
# Wails outputs the binary to desktop/build/bin/ — copy to central dist/ after
# build. NOTE: desktop/build/dist/moxie-desktop is NOT produced by modern
# wails builds; copying from it installs a stale July-2026 artifact.
desktop: dist/$(DESKTOP_BINARY)

dist/$(DESKTOP_BINARY): desktop/build/bin/moxie
	mkdir -p dist
	cp -f desktop/build/bin/moxie dist/$(DESKTOP_BINARY)
	@echo "  -> dist/$(DESKTOP_BINARY)"

desktop/build/bin/moxie: desktop/frontend/src/**/* desktop/app.go desktop/main.go
	cd desktop && wails build -tags webkit2_41

# Run desktop in development mode with hot-reload
desktop-dev:
	cd desktop && wails dev -tags webkit2_41

clean:
	rm -rf dist/
	rm -rf desktop/build/bin/
	rm -rf desktop/frontend/dist/
	rm -rf desktop/frontend/node_modules/
