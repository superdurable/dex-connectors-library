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
- generic HTTP/Webhook and OpenAI Responses connectors, including OpenAI SSE;
- React connection-state components that never receive credential values;
- a deterministic mock provider and real-Dex Customer Onboarding fixtures.

## Quick start

```bash
go test ./...
go run ./cmd/connectorctl validate connectors/http/connector.yaml connectors/openai/connector.yaml
go run ./cmd/connectorctl generate --check connectors/http/connector.yaml
go run ./cmd/connectorctl generate --check connectors/openai/connector.yaml
go run ./cmd/connectorctl catalog connectors

cd sdk/react
npm ci
npm test
npm run build
```

The repository uses `github.com/superdurable/dex/sdk-go v0.10.2` and verifies
integration behavior against Dex Server/dexcli 0.11.1. Those releases are
versioned independently.

Dex CLI 0.11.1 cannot yet render factory Steps in Dex Web 2.0. The Connector
API is intentionally held in Draft for manual acceptance before a separate Dex
CLI analyzer patch.

Read [the Connector contract](docs/connector-contract.md),
[architecture](docs/architecture.md), and
[acceptance guide](docs/acceptance.md) before adding a provider.
