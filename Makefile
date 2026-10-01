.PHONY: check test test-common test-connectors catalog-check test-integration test-sdk-integration test-projectconfig-s3-integration test-connector-integration test-dex-compat-current test-dex-compat-released studio-bundle-theme react react-sdk react-connectors workspace githooks

CONNECTOR_CATALOG ?= catalog.yaml
CONNECTOR_SCOPE ?= all
CONNECTOR_BASE ?=
CONNECTOR_HEAD ?= HEAD
CONNECTOR_SHARD_INDEX ?= 0
CONNECTOR_SHARD_COUNT ?= 1

CONNECTOR_SELECTION_ARGUMENTS = --catalog $(CONNECTOR_CATALOG) --scope $(CONNECTOR_SCOPE) --head $(CONNECTOR_HEAD) --shard-index $(CONNECTOR_SHARD_INDEX) --shard-count $(CONNECTOR_SHARD_COUNT)
ifneq ($(CONNECTOR_SCOPE),all)
CONNECTOR_SELECTION_ARGUMENTS += --base $(CONNECTOR_BASE)
endif

check: test studio-bundle-theme react

test: test-common test-connectors catalog-check

test-common:
	cd sdkgo && GOWORK=off go test -race ./...
	cd sdkgo && GOWORK=off go vet ./...
	python3 -m unittest script/release/component_release_test.py script/dex_compatibility_test.py script/branch_off_main_policy_test.py script/agent_rules_test.py script/studio_bundle_theme_check_test.py
	GOWORK=off go test -race ./...
	GOWORK=off go vet ./...

test-connectors:
	@directories="$$(GOWORK=off go run ./cmd/connectorctl test-matrix $(CONNECTOR_SELECTION_ARGUMENTS))"; \
	for module in $$directories; do \
		python3 script/release/component_release.py validate-connector --component-path "$$module" \
			--sdk-module github.com/superdurable/dex-connectors-library/sdkgo || exit 1; \
		(cd "$$module" && GOWORK=off go test -race ./... && GOWORK=off go vet ./...) || exit 1; \
		GOWORK=off go run ./cmd/connectorctl validate "$$module/connector.yaml" || exit 1; \
		GOWORK=off go run ./cmd/connectorctl generate --check "$$module/connector.yaml" || exit 1; \
	done

catalog-check:
	GOWORK=off go run ./cmd/connectorctl catalog --check --catalog $(CONNECTOR_CATALOG)
	GOWORK=off go run ./cmd/connectorctl release-matrix --catalog $(CONNECTOR_CATALOG)
	GOWORK=off go run ./cmd/connectorctl catalog --catalog $(CONNECTOR_CATALOG) --output /tmp/dex-connectors-catalog.yaml

test-integration: test-sdk-integration test-connector-integration

test-projectconfig-s3-integration:
	cd sdkgo && GOWORK=off go test -race -tags=integration ./projectconfig -run TestAWSProjectConfigurationSnapshotsAndConcurrentAdmission -count=1 -v

test-sdk-integration:
	cd sdkgo && GOWORK=off go test -tags=integration ./integrationtest/... -count=1 -v

test-connector-integration:
	@directories="$$(GOWORK=off go run ./cmd/connectorctl test-matrix $(CONNECTOR_SELECTION_ARGUMENTS))"; \
	for module in $$directories; do \
		(cd "$$module" && GOWORK=off go test -tags=integration ./... -count=1 -v) || exit 1; \
	done

test-dex-compat-current:
	@directories="$$(GOWORK=off go run ./cmd/connectorctl test-matrix $(CONNECTOR_SELECTION_ARGUMENTS) | paste -sd, -)"; \
	python3 script/dex_compatibility.py current --connector-directories "$$directories"

test-dex-compat-released:
	@directories="$$(GOWORK=off go run ./cmd/connectorctl test-matrix $(CONNECTOR_SELECTION_ARGUMENTS) | paste -sd, -)"; \
	python3 script/dex_compatibility.py released --connector-directories "$$directories"

studio-bundle-theme:
	@directories="$$(GOWORK=off go run ./cmd/connectorctl test-matrix $(CONNECTOR_SELECTION_ARGUMENTS))"; \
	python3 script/studio_bundle_theme_check.py . --selected $$directories

react: react-sdk react-connectors

react-sdk:
	cd sdk/react && npm ci && npm test && npm run build

react-connectors:
	@directories="$$(GOWORK=off go run ./cmd/connectorctl test-matrix $(CONNECTOR_SELECTION_ARGUMENTS))"; \
	has_ui=false; \
	for module in $$directories; do \
		if [ -f "$$module/ui/package-lock.json" ]; then has_ui=true; break; fi; \
	done; \
	if [ "$$has_ui" = true ]; then npm ci --prefix sdk/react && npm run build --prefix sdk/react; fi; \
	for module in $$directories; do \
		ui="$$module/ui"; \
		if [ ! -f "$$ui/package-lock.json" ]; then continue; fi; \
		(cd "$$ui" && npm ci && npm test && npm run build) || exit 1; \
		GOWORK=off go run ./cmd/connectorctl ui-artifact --manifest "$$module/connector.yaml" --ui-root "$$ui/dist" \
			--output /tmp/connector-ui.tgz --digest-output /tmp/connector-ui.tgz.sha256 || exit 1; \
	done

workspace:
	@test -f go.work || go work init
	go work use -r .

githooks:
	sh script/install-githooks
