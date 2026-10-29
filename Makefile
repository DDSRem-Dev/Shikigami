SHELL := /bin/bash

.PHONY: tools generate-proto build test test-tools test-linux test-integration test-e2e test-all fmt vet lint clean build-embedded release

tools:
	./scripts/dev-setup.sh

generate-proto:
	./scripts/generate-proto.sh

build:
	./scripts/build.sh

test: test-tools
	go test -race ./...
	go test -race ./scripts/release-trust.go ./scripts/release-trust_test.go
	go test -race ./scripts/release.go ./scripts/release_test.go

test-tools:
	@test "$$(id -u)" -ne 0 || { echo 'Run tool tests as an ordinary user; root would skip collector lifecycle checks.' >&2; exit 1; }
	@python3 -B -c 'import sys; sys.version_info >= (3, 10) or sys.exit("Tool tests require Python 3.10 or newer.")'
	python3 -B -m unittest discover -s scripts/tests -p 'test_*.py' -v

test-linux:
	./scripts/test-linux.sh

test-integration:
	@test -d test/integration || { echo 'Integration suite is not implemented yet.' >&2; exit 1; }
	go test -race -tags=integration ./test/integration/...

test-e2e:
	@test -d test/e2e || { echo 'End-to-end suite is not implemented yet.' >&2; exit 1; }
	go test -race -tags=e2e ./test/e2e/...

test-all: test test-integration test-e2e

fmt:
	gofmt -w $$(git ls-files --cached --others --exclude-standard '*.go')

vet:
	go vet ./...
	go vet ./scripts/release-trust.go ./scripts/release-trust_test.go
	go vet ./scripts/release.go ./scripts/release_test.go

lint: vet
	@test -z "$$(gofmt -l $$(git ls-files --cached --others --exclude-standard '*.go'))" || { echo 'Run make fmt.' >&2; exit 1; }

clean:
	rm -rf bin dist coverage.out

build-embedded:
	./scripts/build-embedded.sh

release:
	./scripts/release.sh
