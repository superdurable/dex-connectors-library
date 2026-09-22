# Dex Connectors Library

Dex Connectors Library is the open-source boundary between Dex Flows and
external providers. It keeps credentials outside durable Flow state, gives
provider calls stable identities, and makes mutation outcomes explicit.

The first alpha includes:

- a versioned connector manifest and `connectorctl` validation/catalog CLI;
- a Dex-native Go SDK contract for credentials, calls, results, receipts, and errors;
- generic HTTP/Webhook and OpenAI Responses API connectors;
- React connection-state components that never receive credential values;
- a deterministic mock provider and a runnable Dex Flow example.

## Quick start

```bash
go test ./...
go run ./cmd/connectorctl validate connectors/http/connector.yaml
go run ./cmd/connectorctl catalog connectors

cd sdk/react
npm ci
npm test
npm run build
```

The example pins the latest independently published Go SDK,
`github.com/superdurable/dex/sdk-go v0.10.2`. Dex Server and dexcli releases
are versioned independently.

See [the architecture](docs/architecture.md) and
[manual acceptance guide](docs/acceptance.md) before integrating a new
provider.
