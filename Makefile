.PHONY: check test test-integration react

check: test react

test:
	cd sdk/go && GOWORK=off go test -race ./...
	cd sdk/go && GOWORK=off go vet ./...
	python3 -m unittest script/release/component_release_test.py
	go test ./...
	go run ./cmd/connectorctl generate --check connectors/http/connector.yaml
	go run ./cmd/connectorctl generate --check connectors/openai/connector.yaml

test-integration:
	go test -tags=integration ./examples/customer-onboarding -count=1 -v
	cd sdk/go && GOWORK=off go test -tags=integration ./integrationtest/... -count=1 -v

react:
	cd sdk/react && npm ci && npm test && npm run build
