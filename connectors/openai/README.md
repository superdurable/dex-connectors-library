# OpenAI Connector

The OpenAI connector is an independent Go module for the Responses API. It
provides:

- `openai.NewCreateResponseStep` for idempotent creation, SSE text, progress,
  usage, and explicit uncertainty;
- `openai.NewRetrieveResponseStep` for query-first reconciliation.

Create one `openai.Connection` from a configured client and logical
`sdkgo.ConnectionRef`, then pass it to the operation-specific factory.
The generated connection type prevents cross-connector wiring and rejects
serialization.

Install a published component release:

```bash
go get github.com/superdurable/dex-connectors-library/connectors/openai@v0.1.0
```

Verify this module independently:

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

Applications own and register the Result Attribute and any structured/text
Dex Streams. Stream data is best-effort; the committed Result and branch are
authoritative.
