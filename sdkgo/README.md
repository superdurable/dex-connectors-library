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

## Trigger delivery outcomes

`HandleTrigger` has three outcomes:

- `nil` consumes the event. The Dex targets return nil when the filter rejects
  the event, when Dex accepts the Flow start or RPC, and when the Flow was
  already started. The RPC target also returns nil when Dex applied the RPC but
  the client cannot decode its response, because a retry would apply it again.
- An `UndeliverableTriggerError` also consumes the event, because no retry can
  deliver it. Wrap an error with `MarkTriggerUndeliverable` to report this, and
  test for it with `IsTriggerUndeliverable`.
- Any other error keeps the event pending for a retry.

The Dex targets report an event as undeliverable only when the event itself
cannot be delivered:

- its Flow completed, failed, stopped, or was never started;
- the RPC handler returned the error from `MarkTriggerUndeliverable`;
- the input cannot be encoded;
- the resolver returned an empty Flow ID, or the event has no ID.

Every other error is retryable. This includes:

- an unavailable Dex Server or Worker, lock conflicts, and timeouts;
- handler panics and plain handler errors;
- a handler that returns another Flow's Dex error unchanged, even though its
  gRPC code is `FailedPrecondition`;
- Flow registration errors, and requests that the Dex Server rejects as
  invalid, such as a Step heartbeat below the server's configured minimum or an
  unknown Attribute Store;
- a Channel message that another update consumed first.

A defect in the application or the server configuration therefore keeps its
events, and they replay once it is fixed.

`DeliverTrigger` retries a retryable error after 250 milliseconds and doubles
the delay up to 30 seconds. It returns as soon as its context ends, even during
a delay. Durable replay uses the same policy, so a Dex outage delays replay but
never ends it. A `dex.Worker` still needs the Dex Server when it starts:
`Worker.Start` fails when the Dex Server is unreachable, so an application that
runs a Worker should wait for the Dex Server first.

A failed attempt may still have been applied: a timeout or a dropped connection
can hide an RPC that Dex already ran. Flow starts are idempotent because the
event ID is the Flow-start request ID. Dex does not deduplicate a retried RPC,
so every application RPC must treat a repeated event ID as a duplicate. If a
retry finds the Flow closed, the event is consumed as undeliverable.

An RPC target cannot tell a reply that overtook its root from a reply in a
thread that never had a Flow, and it consumes both. Deliver a Flow's start
before its RPC events through one ordered runner.

The SDK logs every skip, retry, and replay; see [Trigger logging](#trigger-logging).
To tolerate an early event for a bounded time, wrap the RPC target in a
`TriggerTargetFunc` that returns `errors.Unwrap(err)` for an
`UndeliverableTriggerError` instead, which keeps the event pending. A durable
inbox wraps the application target, so that wrapper also sees replayed
attempts.

The durable inbox retries its own read and write failures the same way and
logs each one at ERROR. If a runner stops delivering, look for those records:
the inbox directory may not be writable, or the disk may be full. When only
the removal of a consumed event fails, the inbox retries the removal without
invoking the target again.

An RPC handler can reject an event for business reasons in two ways. It can
return a normal result, which consumes the event, or it can return
`MarkTriggerUndeliverable(err)` itself. The marker's gRPC status is
`FailedPrecondition` and its message starts with `Trigger event is
undeliverable`, so the classification survives the Worker boundary. Wrap the
reason inside the marker, not the marker inside another error: the RPC target
checks that the Worker's error detail starts with the marker's message.

### Upgrading from v0.8

v0.9.0 changes how failed deliveries are handled. In v0.8 and earlier the
durable inbox kept every event whose target returned an error and retried it
indefinitely. Now the inbox and the Dex targets consume undeliverable events,
including an RPC event whose Flow has not started yet.

If separate runners deliver a Flow's start and its RPC events, an RPC event
that arrives before its Flow starts is now lost instead of retried. The Gmail
connector's generated per-Trigger factories poll independently and work this
way. Choose one:

- Deliver both through one ordered runner. Slack's
  `NewLocalMessageTriggerRunner` is one, and Gmail v0.11.0 adds one with the
  same name.
- Wrap the RPC target so it returns the unwrapped Dex error for a bounded time,
  as described above.

## Trigger logging

Trigger delivery logs through the standard library `log/slog`. It needs no
configuration: without a logger, each record goes to `slog.Default()` as of
that record, so `slog.SetDefault` in `main` routes every record, even when it
runs after the runners are built. To choose a logger, pass it to the API that
emits the record:

- `sdkgo.DeliverTrigger(ctx, target, event, sdkgo.WithTriggerLogger(logger))`;
- `sdkgo.NewDexFlowTriggerTarget(..., sdkgo.WithTriggerLogger(logger))` and
  `sdkgo.NewDexRPCTriggerTarget(..., sdkgo.WithTriggerLogger(logger))`;
- `localconfig.NewDurableTriggerTarget(..., localconfig.WithTriggerLogger(logger))`;
- the `Logger` field of `sdkgo.TriggerConfig` for runners built by `NewTrigger`.

A connector runner that builds durable inboxes should accept a logger in its
own options and pass it on with `localconfig.WithTriggerLogger`; see each
connector's README. When you build a Dex target by hand, attach the binding's
identity with `logger.With("connector", ..., "connection", ..., "trigger", ...,
"binding", ...)`, so its filtered and delivered records carry the same four keys
as the inbox's records.

Two helpers keep the records about one event together:

- `sdkgo.ContextWithTriggerLogAttrs(ctx, attrs...)` adds event-scoped IDs, such
  as a channel or thread ID, to every record written for that context:
  `DeliverTrigger`'s, and those of the inbox, the Dex targets, and `NewTrigger`
  runners inside it. A record keeps its own value for a key it already has.
- `sdkgo.TriggerAttempt(ctx)` is for a source that retries on its own schedule
  instead of with `DeliverTrigger`, such as a poller. Pass the returned context
  to `HandleTrigger`. After a nil result, the returned function reports whether
  an inbox or runner inside the attempt consumed the event as undeliverable and
  logged the skip, so the source logs `trigger delivered after retry` only when
  it reports false. `DeliverTrigger` runs every attempt this way.

| Level | Message | Emitted by | Attributes |
| --- | --- | --- | --- |
| WARN | `trigger event skipped: undeliverable` | whichever component consumes the undeliverable event: the durable inbox, `DeliverTrigger`, or a `NewTrigger` runner | `event_id`, `flow_id` for a Dex error, `error` |
| INFO | `trigger event skipped: filtered` | a Dex target whose `TriggerFilter` returned false | `target`, `event_id` |
| WARN | `trigger delivery failed; retrying` | `DeliverTrigger`, before every backoff | `event_id`, `attempt`, `delay`, `flow_id` for a Dex error, `error` |
| INFO | `trigger delivered after retry` | `DeliverTrigger`, when a later attempt delivers the event | `event_id`, `attempts` |
| DEBUG | `trigger event delivered` | a Dex target, when Dex starts the Flow or applies the RPC | `target`, `event_id`, `flow_id`, and `duplicate` for Flow starts |
| WARN | `trigger event delivered; rpc response undecodable` | the RPC target, when Dex applied the RPC but its response cannot be decoded | `target`, `event_id`, `flow_id`, `error` |
| INFO | `replaying pending trigger events` | the durable inbox, when replay finds pending events | `count` |
| INFO | `finished replaying pending trigger events` | the durable inbox, after that replay | `delivered`, `skipped`, `remaining`, and `error` when the context ended first |
| ERROR | `trigger inbox read failed`, `trigger inbox write failed`, `trigger inbox remove failed` | the durable inbox | `event_id` (except for the read at replay start), `error` |

Attribute keys:

- `connector`, `connection`, `trigger`, and `binding` identify a binding. The
  durable inbox and `NewTrigger` runners add all four to their records, and
  `DeliverTrigger` records inherit them during replay.
- `event_id` is the provider event ID, and `flow_id` is the resolved Flow ID.
  Skip and retry records carry `flow_id` when the error is a Dex error about
  one Flow.
- `target` is `flow_start` or `rpc`.
- `attempt` numbers a failed attempt from 1, and `attempts` counts every
  attempt including the successful one. `delay` is the wait before the next
  attempt, as a Go duration such as `250ms` or `30s`.
- `count` is the number of events pending when replay starts. `delivered` and
  `skipped` count the events replay consumed; `delivered` includes filtered
  events. `remaining` counts the events replay did not reach.
- `error` is the error message.

The component that consumes an event logs its skip, so every skip appears
once. When a durable inbox consumes an event as undeliverable after a failed
attempt, `DeliverTrigger` does not also report it as delivered. A delivery that
succeeds on its first attempt logs only the DEBUG record, and a replay that
finds no pending events logs nothing, so logs at INFO stay quiet unless an event
is skipped, retried, or replayed.

The SDK's own records contain IDs and error messages only: never event
payloads or message text. The `error` attribute holds `err.Error()` as a
string, so a handler cannot reach an error's fields, and the SDK removes the
query string and user information of any URL in a wrapped `*url.Error`. Every
other part of an error message is logged as written. A Dex error message
includes the Flow ID and the Dex Server's or Worker's detail, which for an RPC
handler failure is the handler's error message. Every `TriggerTarget`,
including a `TriggerTargetFunc`, and every RPC handler must therefore keep
message text, credentials, and tokens out of the errors it returns.

Records report the SDK function that wrote them as their source, so a handler
with `AddSource` points at the delivery code rather than a logging helper.

## Connector Steps

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

Generated Step and Trigger binding configs also accept a static
`ConnectorConfigurationUI`. A Flow composes release-owned UI units and binds
their ports to JSON Pointers in its own configuration shape. Dex CLI extracts
only static literals into the Flow Definition; dynamic UI composition is
rejected. UI metadata never contains selected values.

For local operation configuration, load selected values once during startup:

```go
loaded, err := localconfig.LoadOperationConfiguration[ReplyConfiguration](store,
    sdkgo.ConnectorConfigurationRef{
        ConnectorID: "slack", ConnectionName: "workspace", OperationID: "postThreadReply",
        FlowType: "ApprovalFlow", StepType: "PostCompletion",
    })
```

Pass the resulting `ConnectorLoadedConfiguration[ReplyConfiguration]` into the
Flow constructor and explicitly use `loaded.Value` inside
`MapToOperationInput`. The identity includes Flow and Step types, so two uses of
the same operation never share configuration implicitly.

Every operation declares the standard `defect` branch. Mutations declare the
standard `uncertain` branch only when a dispatched provider call can have an
unknown outcome; otherwise generated factories do not require that target.

## Local development

Local development configuration stores named connections and named Trigger
bindings separately. `localconfig.Store.DecodeTriggerConfiguration` selects a
binding by connector ID, connection name, trigger name, and binding name. This
allows several Flows to reuse one provider connection without sharing their
event filters. Generated local Trigger factories also wrap the target in a
binding-specific disk inbox. A source persists a matched event before provider
acknowledgement and replays pending events after a process restart. The inbox
removes an event when the target consumes it: the target returns nil, or it
reports the event undeliverable.

## Release and tests

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
and a progress Stream using only this module's public API. It also drives
Trigger delivery through the local inbox:

- an approval after completion, and an approval in a thread without a Flow;
- a handler rejection, and a handler that returns another Flow's Dex error;
- a Flow start that the Dex Server rejects, replayed once the Flow is fixed;
- an RPC response the client cannot decode;
- replay while the Worker is unavailable;
- the log records for each skip, filter, backoff, recovery, and replay summary,
  with a sentinel that proves message text never reaches a record.

Run it against a real Dex Server:

```bash
GOWORK=off go test -tags=integration ./integrationtest/... -count=1 -v
```
