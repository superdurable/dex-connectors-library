# HTTP Connector

The HTTP connector is an independent Go module. It provides strongly typed
Dex Step factories for bounded Query, idempotent Mutation, and signed webhook
verification:

- `httpconnector.NewQueryStep`
- `httpconnector.NewMutationStep`
- `httpconnector.NewVerifyWebhookStep`

Create one `httpconnector.Connection` from a configured client and logical
`sdkgo.ConnectionRef`, then pass it to operation-specific factory configs.
Connection values cannot be serialized and must never enter Flow input or
durable state.

Install a published component release:

```bash
go get github.com/superdurable/dex-connectors-library/connectors/http@v0.1.0
```

Verify this module independently:

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

The manifest is the source for generated Config, Credentials, branches,
factories, and Dex Step defaults. Run `connectorctl generate --check` from the
repository root after any manifest or generated API change.
