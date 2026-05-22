SHELL := /bin/sh

APP := kubectl-tree
PKG := .

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

BUILD_DIR := build
DIST_DIR := dist

PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

GO_LDFLAGS ?= -s -w

.PHONY: all build clean dist

all: build

build:
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags '$(GO_LDFLAGS)' -o $(BUILD_DIR)/$(APP) $(PKG)

dist: clean
	mkdir -p $(DIST_DIR)
	set -e; \
	for p in $(PLATFORMS); do \
		GOOS=$${p%/*}; GOARCH=$${p#*/}; \
		outdir="$(DIST_DIR)/$(APP)_$(VERSION)_$${GOOS}_$${GOARCH}"; \
		mkdir -p "$$outdir"; \
		bin="$(APP)"; \
		if [ "$$GOOS" = "windows" ]; then bin="$(APP).exe"; fi; \
		CGO_ENABLED=0 GOOS=$$GOOS GOARCH=$$GOARCH go build -trimpath -ldflags '$(GO_LDFLAGS)' -o "$$outdir/$$bin" $(PKG); \
		cp -f LICENSE README.md "$$outdir/"; \
		tar -C $(DIST_DIR) -czf "$(DIST_DIR)/$(APP)_$(VERSION)_$${GOOS}_$${GOARCH}.tar.gz" "$(APP)_$(VERSION)_$${GOOS}_$${GOARCH}"; \
		rm -rf "$$outdir"; \
	done

clean:
	rm -rf $(BUILD_DIR) $(DIST_DIR)
