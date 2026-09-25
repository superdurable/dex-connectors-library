# Connector Go SDK

This module contains the provider-neutral contracts used by Dex connector
modules. It owns stable call identity, typed attempts and results, generic Step
factories, progress Streams, optional Result Attributes, and the canonical
metadata used by operation-specific generated factories.

Applications normally depend on a connector module such as OpenAI rather than
constructing the generic factories directly. Connector authors may use the
generic `NewQueryStep` and `NewMutationStep` APIs as an advanced escape hatch.

Connector Trigger sources run outside Dex Steps and deliver typed, stable-ID
events through `TriggerRunner`. Generated Trigger factories accept any typed
`TriggerTarget`. Applications choose `NewDexFlowTriggerTarget`,
`NewDexRPCTriggerTarget`, or a custom target. Both Dex targets require an
application-owned `TriggerFilter` and `FlowIDResolver`. Flow targets also take a
`FlowInputMapper`; RPC targets take an `RPCInputMapper` and may invoke any typed
application RPC. Every callback receives the same complete Trigger event. The
filter runs before Flow ID resolution and returns false to consume an irrelevant
event without calling Dex. These callbacks are pure functions without error
results. They should be deterministic and side-effect free because a persisted
delivery can be replayed after restart. The SDK uses the provider event ID as
the Flow-start request ID.

Provider Trigger configuration may reduce upstream traffic, but it is not the
application admission boundary. Applications use the typed filter to enforce
their own channel, sender, recipient, text, tenant, or other routing rules for
both Flow starts and RPC invocations.

RPC Trigger targets receive the same direct bound Flow method that the
application registers with `dex.DefineRPC`. The method carries the Flow
instance, durable RPC name, input type, and output type, so no configurable
RPC-name string can drift from registration. The application owns RPC options,
durable state, locking, and event deduplication. Connector Triggers preserve the
provider event ID but do not impose a retention policy or create hidden
Attributes.

Connector Steps use `MapToOperationInput` to map application Step input to one
provider operation input. The mapper cannot fail and the branch target receives
only the current `QueryResult` or `MutationResult`. Applications should use an
initialization Step to validate start input and persist domain context before a
Connector Step. A Result Attribute is always optional. Configure one only when
the raw provider result must remain available outside the transition chain;
otherwise the result is already durable as the branch target's input.
Applications must register every configured Attribute and Stream explicitly in
the Flow persistence schema. The Connector SDK does not aggregate or register
those resources. Manifest progress declarations only control which typed Stream
fields code generation exposes; runtime operation definitions do not duplicate
or validate that metadata.

Every operation declares the standard `failed` branch for deterministic terminal
failures. Applications inspect `Failure.Kind` when recovery depends on whether
the cause is validation, authentication, authorization, provider rejection, or
a local Connector defect. Mutations declare the
standard `uncertain` branch only when a dispatched provider call can have an
unknown outcome; otherwise generated factories do not require that target.

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
