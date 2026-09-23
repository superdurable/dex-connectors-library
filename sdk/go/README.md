# Connector Go SDK

This module contains the provider-neutral contracts used by Dex connector
modules. It owns stable call identity, typed attempts and results, generic Step
factories, progress Streams, Result Attribute requirements, and the canonical
metadata used by operation-specific generated factories.

Applications normally depend on a connector module such as OpenAI rather than
constructing the generic factories directly. Connector authors may use the
generic `NewQueryStep` and `NewMutationStep` APIs as an advanced escape hatch.

The module is released independently with directory-prefixed tags such as
`sdk/go/v0.1.0`. Connector modules must pin an already-published SDK release.

Run its supported checks without the repository workspace:

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```
