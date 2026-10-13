.DEFAULT_GOAL := help
VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: help test build pi package

help: ## List targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-9s %s\n", $$1, $$2}'

test: ## gofmt check, go vet and the tests
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:"; gofmt -l .; exit 1)
	go vet ./...
	go test ./...

build: ## Build for this machine into dist/listener (test-mic, version, ...)
	mkdir -p dist
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/listener ./cmd/listener

package: ## Build the release files into dist/ as a tag would: make package VERSION=v1.2.3
	bash scripts/package.sh $(VERSION)

pi: ## Cross-compile for a Raspberry Pi into dist/now-playing-listener (linux/arm64)
	mkdir -p dist
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/now-playing-listener ./cmd/listener
