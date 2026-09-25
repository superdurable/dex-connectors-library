# Connector Go SDK

This module contains the provider-neutral contracts used by Dex connector
modules. It owns stable call identity, typed attempts and results, generic Step
factories, progress Streams, Result Attribute requirements, and the canonical
metadata used by operation-specific generated factories.

Applications normally depend on a connector module such as OpenAI rather than
constructing the generic factories directly. Connector authors may use the
generic `NewQueryStep` and `NewMutationStep` APIs as an advanced escape hatch.

Connector Trigger sources run outside Dex Steps and deliver typed, stable-ID
events through `TriggerRunner`. Generated Trigger factories accept any typed
`TriggerTarget`. Applications choose `NewDexFlowTriggerTarget`,
`NewDexRPCTriggerTarget`, or a custom target and provide a typed
`FlowIDResolver`. The SDK uses the provider event ID as the Flow-start request
ID.

RPC Trigger targets receive the same direct bound Flow method that the
application registers with `dex.DefineRPC`. The method carries the Flow
instance, durable RPC name, input type, and output type, so no configurable
RPC-name string can drift from registration. The application owns RPC options,
durable state, locking, and event deduplication. Connector Triggers preserve the
provider event ID but do not impose a retention policy or create hidden
Attributes.

Local development configuration stores named connections and named Trigger
bindings separately. `localconfig.Store.DecodeTriggerConfiguration` selects a
binding by connector ID, connection name, trigger name, and binding name. This
allows several Flows to reuse one provider connection without sharing their
event filters. Generated local Trigger factories also wrap the target in a
binding-specific disk inbox. A source persists a matched event before provider
acknowledgement, removes it only after Dex accepts it, and replays pending
events after a process restart.

The module is released independently with directory-prefixed tags such as
`sdkgo/v0.1.0`. Connector modules must pin an already-published SDK release.

Run its supported checks without the repository workspace:

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

The `integrationtest` package behaves like an independently authored
connector. It defines typed Connection and Credentials, Query and Mutation
operations, operation-specific Step factories, branch targets, an Attribute,
and a progress Stream using only this module's public API. Run it against a
real Dex Server:

```bash
GOWORK=off go test -tags=integration ./integrationtest/... -count=1 -v
```
