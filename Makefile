.PHONY: bootstrap fmt whitespace fmt-check lint test test-integration mod-verify build contract-check audit secret-scan secret-scan-test smoke db-up db-down migrate-up migrate-down build-ios test-ios test-ios-integration analyze-ios verify-ios check verify

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

test-integration:
	GO="$(GO)" bash scripts/test-integration.sh

mod-verify:
	cd backend && $(GO) mod verify

build:
	mkdir -p backend/bin
	cd backend && $(GO) build -trimpath -o bin/jarvis-api ./cmd/api
	cd backend && $(GO) build -trimpath -o bin/jarvis-migrate ./cmd/migrate

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

db-up:
	docker compose up --detach --wait --wait-timeout 45 postgres

db-down:
	docker compose down --timeout 10

migrate-up:
	cd backend && $(GO) run ./cmd/migrate up

migrate-down:
	cd backend && $(GO) run ./cmd/migrate down

build-ios:
	xcodebuild build -quiet -project apps/ios/JARVIS.xcodeproj -scheme JARVIS -destination 'generic/platform=iOS Simulator' CODE_SIGNING_ALLOWED=NO

test-ios:
	bash scripts/test-ios.sh --all

test-ios-integration:
	bash scripts/test-ios-integration.sh

analyze-ios:
	xcodebuild analyze -quiet -project apps/ios/JARVIS.xcodeproj -scheme JARVIS -destination 'generic/platform=iOS Simulator' CODE_SIGNING_ALLOWED=NO

verify-ios: build-ios analyze-ios test-ios

check: whitespace fmt-check lint test mod-verify contract-check secret-scan secret-scan-test

verify: bootstrap
	$(MAKE) check
	$(MAKE) build
	$(MAKE) audit
	$(MAKE) test-integration
	$(MAKE) smoke
