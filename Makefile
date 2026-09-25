.PHONY: check test test-integration test-dex-compat-current test-dex-compat-released react

check: test react

test:
	cd sdkgo && GOWORK=off go test -race ./...
	cd sdkgo && GOWORK=off go vet ./...
	@find connectors -name connector.yaml -print | sort | while IFS= read -r manifest; do \
		module=$${manifest%/connector.yaml}; \
		(cd "$$module" && GOWORK=off go test -race ./... && GOWORK=off go vet ./...) || exit 1; \
	done
	python3 -m unittest script/release/component_release_test.py
	go test -race ./...
	go vet ./...
	@find connectors -name connector.yaml -print | sort | while IFS= read -r manifest; do \
		go run ./cmd/connectorctl validate "$$manifest" && \
		go run ./cmd/connectorctl generate --check "$$manifest" || exit 1; \
	done
	go run ./cmd/connectorctl release-workflow --check connectors .github/workflows/release-connector.yml
	go run ./cmd/connectorctl catalog connectors

test-integration:
	cd sdkgo && GOWORK=off go test -tags=integration ./integrationtest/... -count=1 -v
	@find connectors -name connector.yaml -print | sort | while IFS= read -r manifest; do \
		module=$${manifest%/connector.yaml}; \
		(cd "$$module" && GOWORK=off go test -tags=integration ./... -count=1 -v) || exit 1; \
	done

test-dex-compat-current:
	python3 script/dex_compatibility.py current

test-dex-compat-released:
	python3 script/dex_compatibility.py released

react:
	cd sdk/react && npm ci && npm test && npm run build
	@find connectors -path '*/ui/package-lock.json' -print | sort | while IFS= read -r lockfile; do \
		ui=$${lockfile%/package-lock.json}; \
		manifest=$${ui%/ui}/connector.yaml; \
		(cd "$$ui" && npm ci && npm test && npm run build) || exit 1; \
		go run ./cmd/connectorctl ui-artifact --manifest "$$manifest" --ui-root "$$ui/dist" \
			--output /tmp/connector-ui.tgz --digest-output /tmp/connector-ui.tgz.sha256 || exit 1; \
	done
