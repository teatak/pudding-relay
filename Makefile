GO ?= go
RELAY_VERSION := $(shell cat VERSION)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS = -s -w -X main.version=$(RELAY_VERSION) -X main.commit=$(COMMIT)

.PHONY: build run test check fmt

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/pudding-relay ./cmd/pudding-relay

run:
	$(GO) run -ldflags "$(LDFLAGS)" ./cmd/pudding-relay $(ARGS)

test:
	$(GO) test -race ./...

check:
	@test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; exit 1)
	$(GO) vet ./...
	$(GO) test -race ./...

fmt:
	gofmt -w cmd internal

.PHONY: test-install docker-build docker-publish smoke-install

test-install:
	node --test scripts/*.test.cjs

docker-build:
	./scripts/build-image.sh --load

docker-publish:
	./scripts/release.sh current

smoke-install:
	node scripts/install-smoke.cjs

.PHONY: version-patch version-minor version-major release release-current release-patch release-minor release-major

version-patch:
	./scripts/version.sh patch
version-minor:
	./scripts/version.sh minor
version-major:
	./scripts/version.sh major

release: release-patch
release-current:
	./scripts/release.sh current
release-patch:
	./scripts/release.sh patch
release-minor:
	./scripts/release.sh minor
release-major:
	./scripts/release.sh major
