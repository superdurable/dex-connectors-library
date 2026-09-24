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
- generic HTTP/Webhook, OpenAI Responses, Google Sheets, and Gmail connectors;
- credential-free React primitives and a sandboxed Studio setup protocol;
- a deterministic mock provider and real-Dex Customer Onboarding fixtures.

## Quick start

```bash
cd sdk/go
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

cd connectors/http
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
cd ../openai
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
cd ../..

go test ./...
go run ./cmd/connectorctl validate connectors/http/connector.yaml connectors/openai/connector.yaml
go run ./cmd/connectorctl generate --check connectors/http/connector.yaml
go run ./cmd/connectorctl generate --check connectors/openai/connector.yaml
go run ./cmd/connectorctl release-workflow --check connectors .github/workflows/release-connector.yml
go run ./cmd/connectorctl catalog connectors

cd sdk/react
npm ci
npm test
npm run build
```

The Go SDK and every connector are independent Go modules.
Their tags include `sdk/go/vX.Y.Z`, `connectors/http/vX.Y.Z`,
`connectors/openai/vX.Y.Z`, `connectors/google/spreadsheet/vX.Y.Z`, and
`connectors/google/gmail/vX.Y.Z`. The historical root `v0.1.0` tag does not
version any standalone component. Git tags are the release version source of
truth.

The SDK must be released before a connector can pin a new SDK version. Each
connector currently pins the published SDK `v0.1.0`. See
[the versioning and release guide](docs/versioning-and-releases.md).

The Connector Go SDK uses `github.com/superdurable/dex/sdk-go v0.11.3` and verifies
integration behavior against Dex Server/dexcli 0.11.3. Those releases are
versioned independently.

Dex CLI 0.11.3 renders canonical Connector factory Steps, branch transitions,
Result Attributes, and progress Streams in Dex Web 2.0. Connector releases
with Studio metadata also publish a deterministic UI tarball for a trusted
Studio BFF to verify and serve in a sandbox iframe.

Read [the Connector contract](docs/connector-contract.md),
[architecture](docs/architecture.md),
[manifest authoring guide](docs/manifest-authoring.md), and
[acceptance guide](docs/acceptance.md) before adding a provider.
