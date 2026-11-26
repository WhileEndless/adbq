# adbq — Makefile
#
# Wails v2 + Go backend + React/TS frontend. Targets group naturally:
#   make            → help
#   make dev        → wails dev (hot-reload, opens app)
#   make build      → wails build (release binary in build/bin/)
#   make build-prod → stripped + UPX-skipped release with -trimpath
#   make install    → build and install/update the macOS app
#   make test       → Go tests + frontend type-check
#   make lint       → gofmt, go vet, staticcheck (if installed), tsc, eslint (if installed)
#   make tidy       → go mod tidy + npm install
#   make generate   → regenerate Wails bindings (frontend/wailsjs/**)
#   make clean      → remove build artifacts
#   make doctor     → wails doctor (verify toolchain)
#   make tools      → install dev tools (wails CLI, govulncheck, staticcheck)
#
# Per CLAUDE.md, lint + test + wails doctor must be green before commit.

# ─── Config ─────────────────────────────────────────────────────────────────
APP_NAME      := adbq
BIN_DIR       := build/bin
FRONTEND_DIR  := frontend
GO_PKGS       := ./...
WAILS         := wails
GO            := go
NPM           := npm

# Single source of truth for the version: internal/version/version.go. Stamped
# into release builds so the binary, `adbq --version`, and the git tag agree.
VERSION       := $(shell awk -F'"' '/Version = "/{print $$2; exit}' internal/version/version.go)

# Wails build flags. -trimpath strips local paths from binaries; -s -w cuts
# debug symbols for smaller release binaries.
LDFLAGS_PROD  := -s -w -X adbq/internal/version.Version=$(VERSION)
GO_FLAGS_PROD := -trimpath
# Wails v2.12's embed scan can fail with newer Go import data. Prepare our
# known embed path directly and keep Wails binding generation enabled.
WAILS_BUILD   := $(WAILS) build -skipembedcreate
INSTALL_DIR   ?= /Applications

# Detect host OS for nicer "build all" affordances. Cross-compiling .app
# bundles between macOS and Linux/Windows is not supported by Wails — build each
# target on its own OS (locally or via .github/workflows/release.yml).
HOST_OS := $(shell uname -s | tr '[:upper:]' '[:lower:]')

# ─── Phony ──────────────────────────────────────────────────────────────────
.PHONY: help all version dev build build-prod build-debug build-target install prepare-embed \
        build-mac build-mac-intel build-mac-arm build-universal \
        build-linux build-windows \
        test test-go test-frontend vet fmt lint tidy generate \
        clean clean-deps doctor tools install-deps \
        run frontend-install frontend-dev frontend-build \
        vuln staticcheck

.DEFAULT_GOAL := help

# ─── Help ───────────────────────────────────────────────────────────────────
help: ## Show this help.
	@echo "adbq — common targets:"
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# ─── Develop ────────────────────────────────────────────────────────────────
dev: frontend-install ## Run the app with hot-reload (wails dev).
	$(WAILS) dev

run: build ## Build then launch the release binary.
ifeq ($(HOST_OS),darwin)
	open $(BIN_DIR)/$(APP_NAME).app
else
	$(BIN_DIR)/$(APP_NAME)
endif

# ─── Build ──────────────────────────────────────────────────────────────────
build: frontend-install prepare-embed ## Default debug-friendly build (build/bin/).
	$(WAILS_BUILD)

build-prod: frontend-install prepare-embed ## Release build with stripped symbols + trimpath.
	$(WAILS_BUILD) -clean $(GO_FLAGS_PROD) -ldflags "$(LDFLAGS_PROD)"

build-debug: frontend-install prepare-embed ## Debug build (devtools open, symbols kept).
	$(WAILS_BUILD) -debug -devtools

ifeq ($(HOST_OS),darwin)
install: export ADBQ_INSTALL_DIR = $(INSTALL_DIR)
install: build-prod ## Build and install or update adbq.app (default: /Applications).
	sh build/install-macos.sh "$(BIN_DIR)/$(APP_NAME).app" "$$ADBQ_INSTALL_DIR"
else
install: ## Install adbq.app (macOS only).
	@echo "make install is available on macOS." >&2
	@exit 1
endif

version: ## Print the version (single source: internal/version/version.go).
	@echo "$(VERSION)"

# Generic escape hatch: build any Wails target directly, e.g.
#   make build-target PLATFORM=darwin/amd64
#   make build-target PLATFORM=windows/arm64
# Cross-ARCH within the SAME OS works (e.g. amd64 on an Apple-Silicon Mac).
# Cross-OS (e.g. a Linux binary from macOS) is NOT supported by Wails and will
# fail at the CGO/webview step — use CI (release.yml) for those.
PLATFORM ?=
build-target: frontend-install prepare-embed ## Build a specific target: make build-target PLATFORM=os/arch.
	@test -n "$(PLATFORM)" || { echo "set PLATFORM, e.g. make build-target PLATFORM=darwin/amd64" >&2; exit 1; }
	$(WAILS_BUILD) -platform $(PLATFORM) -clean $(GO_FLAGS_PROD) -ldflags "$(LDFLAGS_PROD)"

# ─── macOS architecture shortcuts (work on any Mac) ──────────────────────────
build-mac: build-universal ## Alias for build-universal (arm64 + amd64 .app).

build-mac-intel: frontend-install prepare-embed ## macOS Intel/x86_64 (.app). Works on Apple Silicon too.
	$(WAILS_BUILD) -platform darwin/amd64 -clean $(GO_FLAGS_PROD) -ldflags "$(LDFLAGS_PROD)"

build-mac-arm: frontend-install prepare-embed ## macOS Apple Silicon/arm64 (.app).
	$(WAILS_BUILD) -platform darwin/arm64 -clean $(GO_FLAGS_PROD) -ldflags "$(LDFLAGS_PROD)"

build-universal: frontend-install prepare-embed ## macOS universal2 (arm64 + amd64). macOS-only.
ifeq ($(HOST_OS),darwin)
	$(WAILS_BUILD) -platform darwin/universal -clean $(GO_FLAGS_PROD) -ldflags "$(LDFLAGS_PROD)"
else
	@echo "build-universal is macOS-only; use 'make build' on $(HOST_OS)" >&2; exit 1
endif

# ─── Other-OS builds: native only. Use 'make build-target' to force, or CI. ───
build-linux: frontend-install prepare-embed ## Linux amd64 build. Run on Linux (needs gtk3 + webkit2gtk).
ifeq ($(HOST_OS),linux)
	$(WAILS_BUILD) -platform linux/amd64 -clean $(GO_FLAGS_PROD) -ldflags "$(LDFLAGS_PROD)"
else
	@echo "build-linux must run on Linux (cross-OS unsupported). Use CI, or force with 'make build-target PLATFORM=linux/amd64'." >&2; exit 1
endif

build-windows: frontend-install prepare-embed ## Windows amd64 build. Run on Windows.
ifeq ($(HOST_OS),windows)
	$(WAILS_BUILD) -platform windows/amd64 -clean $(GO_FLAGS_PROD) -ldflags "$(LDFLAGS_PROD)"
else
	@echo "build-windows must run on Windows (cross-OS unsupported). Use CI, or force with 'make build-target PLATFORM=windows/amd64'." >&2; exit 1
endif

# ─── Frontend convenience ───────────────────────────────────────────────────
prepare-embed:
	@mkdir -p $(FRONTEND_DIR)/dist
	@test -f $(FRONTEND_DIR)/dist/.gitkeep || touch $(FRONTEND_DIR)/dist/.embed-ready

frontend-install: $(FRONTEND_DIR)/node_modules/.package-lock.json ## Install npm deps if missing.

$(FRONTEND_DIR)/node_modules/.package-lock.json: $(FRONTEND_DIR)/package.json
	cd $(FRONTEND_DIR) && $(NPM) install
	@touch $@

frontend-dev: frontend-install ## Run only the Vite dev server (no Wails).
	cd $(FRONTEND_DIR) && $(NPM) run dev

frontend-build: frontend-install ## Build only the frontend bundle (no Go).
	cd $(FRONTEND_DIR) && $(NPM) run build

# ─── Test / Lint ────────────────────────────────────────────────────────────
test: test-go test-frontend ## Run all tests.

test-go: ## Run Go tests.
	$(GO) test $(GO_PKGS)

test-frontend: frontend-install ## Type-check the frontend (no emit).
	cd $(FRONTEND_DIR) && npx tsc --noEmit

vet: ## go vet across the module.
	$(GO) vet $(GO_PKGS)

fmt: ## Format Go sources (gofmt -w).
	gofmt -w .

lint: vet test-frontend ## All non-test quality gates that don't need installation.
	@if command -v staticcheck >/dev/null; then staticcheck $(GO_PKGS); else echo "staticcheck not installed; skipping (make tools to install)"; fi
	@gofmt_diff=$$(gofmt -l . | grep -v '^build/'); \
	if [ -n "$$gofmt_diff" ]; then echo "Files need gofmt:"; echo "$$gofmt_diff"; exit 1; fi

staticcheck: ## Run staticcheck (install via make tools).
	staticcheck $(GO_PKGS)

vuln: ## Run govulncheck (install via make tools).
	govulncheck $(GO_PKGS)

# ─── Tidy / Generate ────────────────────────────────────────────────────────
tidy: ## go mod tidy + npm install.
	$(GO) mod tidy
	cd $(FRONTEND_DIR) && $(NPM) install

generate: ## Regenerate Wails bindings (frontend/wailsjs/**).
	$(WAILS) generate module

# ─── Hygiene ────────────────────────────────────────────────────────────────
clean: ## Remove build outputs.
	rm -rf $(BIN_DIR)
	rm -rf $(FRONTEND_DIR)/dist

clean-deps: clean ## Also wipe node_modules + Go build cache (safe but slow).
	rm -rf $(FRONTEND_DIR)/node_modules
	$(GO) clean -cache -testcache

doctor: ## wails doctor (verify the toolchain is healthy).
	$(WAILS) doctor

# ─── Tools ──────────────────────────────────────────────────────────────────
tools: ## Install dev tools (Wails CLI, staticcheck, govulncheck).
	$(GO) install github.com/wailsapp/wails/v2/cmd/wails@latest
	$(GO) install honnef.co/go/tools/cmd/staticcheck@latest
	$(GO) install golang.org/x/vuln/cmd/govulncheck@latest
	@echo "Make sure $$(go env GOPATH)/bin is on your PATH."

all: lint test build ## Everything CI would run.
