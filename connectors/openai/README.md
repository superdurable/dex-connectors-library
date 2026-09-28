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
go get github.com/superdurable/dex-connectors-library/connectors/openai@v0.6.0
```

Verify this module independently:

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

Applications own and register the Result Attribute and any structured/text
Dex Streams. Stream data is best-effort; the committed Result and branch are
authoritative.

`CreateResponse` keeps `failed` for an actual failed or incomplete OpenAI
response. Other conclusive API refusals use `providerRejected`.
`RetrieveResponse` additionally exposes `notFound` and `invalidResponse`.
Invalid local input or connection configuration uses the standard `defect`
branch, while an ambiguous dispatched mutation uses `uncertain`.

When a factory supplies progress or text Streams, `CreateResponse` sends
`stream=true`. Its bounded SSE reader accepts lifecycle, text-delta, completed,
failed, incomplete, and error events while ignoring unknown event types. The
completed Response is authoritative for value and usage. Early EOF or a
missing terminal event is uncertain and can be reconciled with
`RetrieveResponse`.
