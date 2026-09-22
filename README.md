# Dex Connectors Library

Dex Connectors Library is the open-source boundary between Dex Flows and
external providers. It keeps credentials outside durable Flow state, derives
provider call identity from Dex Step execution identity, and makes every
provider outcome explicit.

The first alpha includes:

- a versioned connector manifest and deterministic `connectorctl` catalog;
- a Dex-native Go SDK with strict Attempt classification, stable Call IDs,
  provider-specific idempotency keys, safe receipts, and application-owned Dex
  Stream progress;
- generic HTTP/Webhook and OpenAI Responses connectors, including OpenAI SSE;
- React connection-state components that never receive credential values;
- a deterministic mock provider and real-Dex Customer Onboarding fixtures.

## Quick start

```bash
go test ./...
go run ./cmd/connectorctl validate connectors/http/connector.yaml connectors/openai/connector.yaml
go run ./cmd/connectorctl catalog connectors

cd sdk/react
npm ci
npm test
npm run build
```

The repository uses `github.com/superdurable/dex/sdk-go v0.10.2` and verifies
integration behavior against Dex Server/dexcli 0.11.1. Those releases are
versioned independently.

Read [the Connector contract](docs/connector-contract.md),
[architecture](docs/architecture.md), and
[acceptance guide](docs/acceptance.md) before adding a provider.
