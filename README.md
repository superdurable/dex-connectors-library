# Dex Connectors Library

Dex Connectors Library is the open-source boundary between Dex Flows and
external providers. It keeps credentials outside durable Flow state, derives
provider call identity from Dex Step execution identity, and makes every
provider outcome explicit.

The first alpha includes:

- a versioned manifest that generates typed Config, Credentials, branch
  definitions, and operation Step defaults;
- Dex-native Query/Mutation Step factories with typed branch targets, Result
  Attributes, stable Call IDs, idempotency keys, and application-owned Streams;
- GitHub and LinkedIn signup profile, GitHub public-repository, OpenAI
  Responses, Google Sheets, Gmail, and Slack connectors,
  including OpenAI SSE;
- credential-free React primitives and a sandboxed Studio setup protocol;
- a strict local-development connection file loader with generated named Connection helpers;
- real-Dex integration fixtures for public provider connectors.

## Quick start

```bash
cd sdkgo
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
cd ../..

cd connectors/github
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
cd ../..

cd connectors/google/spreadsheet
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
cd ui
npm ci
npm test
npm run build
cd ../../../..

cd connectors/google/gmail
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
cd ui
npm ci
npm test
npm run build
cd ../../../..

cd connectors/linkedin
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
cd ../openai
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
cd ../..

cd connectors/slack
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
cd ui
npm ci
npm test
npm run build
cd ../../..

go test ./...
go run ./cmd/connectorctl validate connectors/github/connector.yaml connectors/google/gmail/connector.yaml connectors/google/spreadsheet/connector.yaml connectors/linkedin/connector.yaml connectors/openai/connector.yaml connectors/slack/connector.yaml
go run ./cmd/connectorctl generate --check connectors/github/connector.yaml
go run ./cmd/connectorctl generate --check connectors/google/gmail/connector.yaml
go run ./cmd/connectorctl generate --check connectors/google/spreadsheet/connector.yaml
go run ./cmd/connectorctl generate --check connectors/linkedin/connector.yaml
go run ./cmd/connectorctl generate --check connectors/openai/connector.yaml
go run ./cmd/connectorctl generate --check connectors/slack/connector.yaml
go run ./cmd/connectorctl release-workflow --check connectors .github/workflows/release-connector.yml
go run ./cmd/connectorctl catalog connectors

cd sdk/react
npm ci
npm test
npm run build
```

The Go SDK and every connector are independent Go modules.
Their tags are directory-prefixed, including `sdkgo/vX.Y.Z`,
`connectors/github/vX.Y.Z`,
`connectors/linkedin/vX.Y.Z`, `connectors/openai/vX.Y.Z`,
`connectors/google/spreadsheet/vX.Y.Z`, and `connectors/google/gmail/vX.Y.Z`.
Slack uses `connectors/slack/vX.Y.Z`.
The
historical root `v0.1.0` tag does not version any standalone component. Git
tags are the release version source of truth.

The SDK must be released before a connector can pin a new SDK version. New
connectors pin the published SDK `v0.1.0`. See
[the versioning and release guide](docs/versioning-and-releases.md).

## Local Dex Web connections

Dex Web writes local-development connections to a JSON file and prints its
absolute path. Set that path before starting the application:

```bash
DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json" go run ./cmd/app
```

Load the file once during application startup, then construct each generated
Connection by name:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
sender, err := gmail.NewLocalConnection(store, "sender")
```

Configuration is snapshotted when `NewLocalConnection` runs, so changing it
requires an application restart. Credentials are reloaded before every
provider call, so reauthorization takes effect without restarting. The loader
strictly rejects unknown JSON fields, duplicate names, symlink files, expired
credentials, and a runtime Connection name that differs from the generated
factory's `ConnectionName`. See the
[runnable local configuration example](examples/local-config/main.go).

The file is a plaintext secret store intended only for local development.
`SecretString` still prevents credentials from being serialized, formatted,
or placed in Flow state by application code.

Connector modules pin an exact released Dex Go SDK version. That build-time
dependency is independent from the compatibility gate, which dynamically
selects the newest stable `cli-v*` release and its embedded Dex Web:

```bash
make test-dex-compat-current
make test-dex-compat-released
```

Current mode packages the working tree as exact test-only Go module versions.
Released mode downloads the newest stable component releases and their real
metadata and UI assets. Set `DEX_CLI_VERSION=vX.Y.Z` to reproduce a Dex failure,
or `CONNECTOR_RELEASE_TAG=connectors/slack/vX.Y.Z` to retest one published
connector. Connector releases with Studio metadata publish a deterministic UI
tarball for Dex Web to verify and serve in a sandbox iframe.

Read [the Connector contract](docs/connector-contract.md),
[architecture](docs/architecture.md),
[manifest authoring guide](docs/manifest-authoring.md), and
[acceptance guide](docs/acceptance.md) before adding a provider.
