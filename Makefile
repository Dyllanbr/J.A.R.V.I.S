.PHONY: bootstrap fmt whitespace fmt-check lint test mod-verify build contract-check audit secret-scan secret-scan-test smoke check verify

GO ?= go
GOFMT ?= gofmt
NPM ?= npm

bootstrap:
	cd qa/playwright && $(NPM) ci

fmt:
	cd backend && $(GO) fmt ./...
	cd qa/playwright && $(NPM) run format

whitespace:
	bash scripts/check-whitespace.sh

fmt-check:
	test -z "$$($(GOFMT) -l backend)"
	cd qa/playwright && $(NPM) run format:check

lint:
	cd backend && $(GO) vet ./...
	cd qa/playwright && $(NPM) run lint
	cd qa/playwright && $(NPM) run typecheck

test:
	cd backend && $(GO) test -race -coverprofile=coverage.out ./...

mod-verify:
	cd backend && $(GO) mod verify

build:
	mkdir -p backend/bin
	cd backend && $(GO) build -trimpath -o bin/jarvis-api ./cmd/api

contract-check:
	cd qa/playwright && $(NPM) run openapi:validate

audit:
	cd qa/playwright && $(NPM) audit --audit-level=high

secret-scan:
	bash scripts/check-secrets.sh

secret-scan-test:
	bash scripts/check-secrets.test.sh

smoke: build
	bash scripts/smoke-test.sh

check: whitespace fmt-check lint test mod-verify contract-check secret-scan secret-scan-test

verify: bootstrap
	$(MAKE) check
	$(MAKE) build
	$(MAKE) audit
	$(MAKE) smoke
