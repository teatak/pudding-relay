GO ?= go
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS = -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: build run test check fmt

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/pudding-relay ./cmd/pudding-relay

run:
	$(GO) run ./cmd/pudding-relay

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
	node --test scripts/install.test.cjs

docker-build:
	./scripts/build-image.sh --load

docker-publish:
	./scripts/build-image.sh --platform linux/amd64,linux/arm64 --push

smoke-install:
	node scripts/install-smoke.cjs
