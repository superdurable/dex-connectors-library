# Anthropic connector

This first Go release exposes `getModel`, a read-only Anthropic Models API Query. It resolves a Claude model ID or alias and returns the resolved ID, display name, and creation timestamp. The connector pins Anthropic API version `2026-10-21`. It sends the API key only in the `Authorization` header. The provider reference is [Get a Model](https://platform.claude.com/docs/en/api/models/retrieve).

`createMessage` and file upload are proposed follow-up capabilities; neither is implemented by this release. No Python or TypeScript package is declared yet. Their implementations follow the Go-verified provider scenarios and separate language releases.

## Authorization

Create an API key at [Claude Platform Settings > API Keys](https://platform.claude.com/settings/keys) scoped to one workspace whose models this Flow should inspect. Save it in Dex Web Connections under the static `anthropic` connection name. The key is a secret; never put it in Flow input. The manifest contains the full setup guide and safe defaults.

## Go

Module: `github.com/superdurable/dex-connectors-library/connectors/anthropic/go` at `v0.1.0` after release. Use `NewProjectConnection` and the generated `NewGetModelStep` factory. The [runnable model inspection Flow](go/examples/model-inspection) shows registration, a Worker entrypoint, FDG generation, and the static `ConnectionName`.

The `found` branch completes the example Flow. `notFound`, `providerRejected`, `invalidResponse`, and `defect` are optional branches; the example leaves them unwired, so a selected branch fails the Flow. Rate limits and temporary provider failures Retry under the default one-hour policy.

## Verification

From `go/`, run `GOWORK=off go test -race ./...` and `GOWORK=off go vet ./...`. The `integration` build tag runs a mock-backed example against a real Dex stack. No live Anthropic authorization or provider call was performed during initial development.
