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

When the sidecar holds nothing for that identity, for example because nobody
has saved the Step's configuration in Dex Web yet, the error wraps
`localconfig.ErrConfigurationNotFound`. Test for it with `errors.Is` to fall
back to a default; every other error means the saved value is invalid. The
message text is unchanged from v0.9.

Every operation declares the standard `defect` branch. Mutations declare the
standard `uncertain` branch only when a dispatched provider call can have an
unknown outcome; otherwise generated factories do not require that target.

## Text generation connectors

Every lab connector exposes the same `generateText` Query, built on a shared
framework in three subpackages instead of its own request pipeline:

- `providerhttp` holds provider-neutral HTTP safety helpers: a client copy that
  never follows redirects, base-URL and header-safe credential checks, bounded
  body reads, `Retry-After` parsing capped at one hour, error-token extraction
  that never returns message text, and a bounded server-sent event reader.
- `llm` holds the contract types (`TextGenerationRequest`,
  `TextGenerationResponse`, `Usage`, finish reasons, reasoning effort), the six
  branch IDs, the model-ID rules and precedence, the `ErrorRule` table, the
  portable structured-output subset with its transforms and post-validation,
  and `TextGenerationQuery`, the pipeline that implements
  `sdkgo.Query[TextGenerationRequest, TextGenerationResponse]`.
- `llm/openaichat` is the OpenAI-compatible Chat Completions wire format,
  configured by a declarative `Profile`.

The root package stays provider-neutral. A subpackage holds a wire format only
when at least two connectors use it, and no `sdkgo` package contains a provider
host, model ID, error-code value, or credential: those stay in each connector's
manifest and profile. The contract is specified in
[Text generation connectors](../docs/connector-contract.md#text-generation-connectors).

### Build a generateText Query

A connector aliases the contract types and builds its Query once per client.
The fixture connector in
[`integrationtest/fixturellm/client.go`](integrationtest/fixturellm/client.go)
declares its provider's dialect:

```go
var chatProfile = openaichat.Profile{
	ProviderName:               ConnectorID,
	ChatCompletionsPath:        "/v1/chat/completions",
	InstructionsRole:           openaichat.InstructionsRoleDeveloper,
	MaxTokensField:             openaichat.MaxTokensFieldMaxCompletionTokens,
	Temperature:                llm.TemperatureRange(0, 1.5),
	StructuredOutput:           llm.StructuredOutputRules{Mode: llm.StructuredOutputModeJSONSchema},
	ShouldSendStrictJSONSchema: true,
	Streaming:                  openaichat.StreamingPolicyAlways,
	ShouldRequestStreamUsage:   true,
	ErrorRules: []llm.ErrorRule{
		{StatusCode: http.StatusTooManyRequests, ErrorToken: "fixture_quota_exhausted", Outcome: llm.QuotaExhaustedOutcome()},
		{StatusCode: http.StatusBadRequest, ErrorToken: "fixture_content_filter", Outcome: llm.BlockedOutcome()},
	},
	RateLimitHeaders: []string{"x-ratelimit-remaining-requests"},
	ModelRules: []openaichat.ModelRule{{
		ModelIDPrefix: "fixture-reasoner", Temperature: &temperatureNotAccepted,
		ReasoningEfforts: map[llm.ReasoningEffort]string{llm.ReasoningEffortLow: "low", llm.ReasoningEffortHigh: "high"},
	}},
}
```

and returns the pipeline from its hand-written client, where the generated
Step factory finds it:

```go
wireFormat, err := openaichat.NewWireFormat(&chatProfile)
if err != nil {
	return nil, err
}
generateText, err := llm.NewTextGenerationQuery(&llm.TextGenerationQueryConfig{
	Definition: GenerateTextDefinition, WireFormat: wireFormat,
	BaseURL: config.Endpoint, ConnectionModel: config.Model,
	HTTPClient: resolved.httpClient, RequestTimeout: requestTimeout,
	ResolveCredential: func(call sdkgo.Call) (sdkgo.SecretString, error) {
		credential, err := credentials.Resolve(call)
		return credential.APIKey, err
	},
	MaxResponseBytes:    cmp.Or(config.MaxResponseBytes, defaultMaxResponseBytes),
	MaxStreamEventBytes: maxStreamEventBytes,
})
if err != nil {
	return nil, fmt.Errorf("fixture-llm: %w", err)
}
return &Client{generateText: generateText}, nil
```

```go
func (client *Client) GenerateText() *llm.TextGenerationQuery {
	return client.generateText
}
```

`NewTextGenerationQuery` requires the operation ID `generateText`, exactly
the branches of `llm.TextGenerationBranchDefinitions()`, and Step defaults
with sync Execute durability, a heartbeat timeout of zero or at least 10
seconds, and an Execute timeout longer than the request timeout. It trims the
connection model, so a blank one means every request must name a model. The
wire format carries the model-ID rule, because it follows from where the
model travels: `openaichat` sends it in the body and uses
`llm.ModelIDRuleBody`. The constructor copies the wire format's map and
slices; its functions and any state they capture stay shared. A native API,
such as Claude Messages, supplies an `llm.WireFormat` struct of functions from
its own module instead of a Profile; the pipeline, error table, schema checks,
and event reader stay shared.

Every Invoke runs the same order: resolve and validate the model, validate the
request against the wire format's declared `RequestFeatures` and the model's
rules, check and transform the structured-output schema, resolve a header-safe
credential, build the request, dispatch it, classify a non-2xx status, then
decode, map the finish reason, join non-reasoning text, and post-validate
structured output against the application's original schema. Every step
before dispatch selects `defect` without a provider request, and a non-zero
request field the wire format does not declare also selects `defect`, so an
older connector never silently ignores a newer field.

| Outcome | Branch or Retry | Failure kind |
| --- | --- | --- |
| Normal finish with text | `generated` | none |
| Output token limit | `truncated`, with any partial text | `RESPONSE_TOO_LARGE` |
| Content policy or refusal, or an error that a rule maps with `llm.BlockedOutcome()`, such as a 400 `content_filter` | `blocked`, without text | `PROVIDER_REJECTION` |
| 401, 403, 404, 409, 501, and other 4xx | `providerRejected` | authentication, authorization, not found, conflict, or rejection |
| 402, or a quota rule such as a billing 429 | `providerRejected` | `QUOTA_EXHAUSTED`; never retried |
| 408, 429, 5xx except 501, a dropped connection, an interrupted event stream, a stall | Retry, after the provider's delay when it sends one | availability, rate limit, or transport |
| Malformed, oversized, or schema-mismatched response, or an unmatched 2xx error object | `invalidResponse` | protocol or response too large |
| Invalid model, request, schema, or credential | `defect`, zero requests | validation, authentication, or local defect |

A streaming request answered with a 2xx body that is not `text/event-stream`,
as from a gateway that ignores streaming, is decoded as a complete response,
so a finished generation or a 2xx error object is classified instead of being
retried as an interrupted stream. A structured-output mismatch names the JSON
pointer, never the value, and a returned number longer than 256 characters or
with an exponent beyond 400 selects `invalidResponse` before any exact
arithmetic. Failures and Receipts never contain prompts, provider message
text, or credentials; a provider model, response ID, or finish token that
contains the credential selects `invalidResponse`. A Receipt carries the Call
ID, provider, response ID, request ID, and only the rate-limit headers the
profile lists.

While an attempt is in flight, the pipeline records a nil Dex heartbeat every
5 seconds, so every valid `heartbeatTimeout`, including Dex's 10-second
minimum, detects a lost Worker even while a non-streaming provider stays
silent. The heartbeat stops before Invoke returns. It proves only that the
Worker's attempt is alive, so the request timeout, which should be the
Execute timeout minus 30 seconds, bounds a hung provider. A profile's
`StallTimeout` also returns Retry when no byte, counting keep-alive comments,
arrives for that long. A nil heartbeat clears any heartbeat checkpoint, so a
business Step that runs the Query through `sdkgo.RunQuery` must not rely on
its own heartbeat checkpoints. Keep the Step sync: an application override to
async durability sends a call that outlasts the seven-second local phase to
the provider a second time.

Streamed text reaches the Step's text Stream as it arrives, before the finish
reason is known, and a retry does not remove it. After a Retry the text
Stream can hold the interrupted attempt's partial text followed by the whole
text of the next attempt, and a streamed attempt that selects `blocked` or
`invalidResponse` can leave text on the Stream although its Result has none.
The Result's `Text` is the only authoritative text.

### Choose the model

The request's `Model`, trimmed, wins; a blank one uses the connection's model.
`RequestedModel` is set on every branch once the model is valid, and
`ServedModel` is the provider's echo. `llm.ModelIDRuleBody` accepts 1 to 256
bytes of printable ASCII, and `llm.ModelIDRulePathSegment` accepts a URL path
segment after stripping one `models/`. The shared cases in
[`llm/llmtest/testdata/model_id_cases.json`](llm/llmtest/testdata/model_id_cases.json)
pin both rules for the TypeScript picker.

An application reads a Step's model pick once at startup with
`localconfig.LoadOperationConfiguration`. An error that matches
`errors.Is(err, localconfig.ErrConfigurationNotFound)` means no pick was
saved, so the Step inherits the connection's model. The Step's
`MapToOperationInput` then sets the loaded pick as the request's `Model`, and
an empty pick also inherits the connection's model.

### Prove a connector

`llm/llmtest` holds the conformance kit. `RunTextGenerationExchangeSuite` runs
the provider-exchange cases against a credential-safe fake provider without
Dex, and `RunTextGenerationDexScenarios` runs one-Step Flows through a real
Worker. Both take closures, because each connector generates its own
Connection and Step config types. A Chat Completions connector describes its
replies with `openaichattest.NewProviderDialect`, as the fixture connector does
in [`integrationtest/fixturellm/exchange_test.go`](integrationtest/fixturellm/exchange_test.go):

```go
var fixtureDialect = openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
	ConnectionModel: "fixture-model-a", AlternateModel: "fixture-reasoner-b", IsStreaming: true,
	QuotaExhaustedStatusCode: http.StatusTooManyRequests, QuotaExhaustedErrorToken: "fixture_quota_exhausted",
	ContentPolicyErrorToken: "fixture_content_filter",
})
```

```go
func TestGenerateTextFollowsTheExchangeContract(t *testing.T) {
	temperatureAboveRange, temperature := 1.6, 0.2
	llmtest.RunTextGenerationExchangeSuite(t, &llmtest.TextGenerationExchangeSuite{
		Dialect:  fixtureDialect,
		NewQuery: newFixtureQuery,
		LocallyRejectedRequests: []llmtest.NamedTextGenerationRequest{
			{Name: "temperature above the model range", Request: llm.TextGenerationRequest{Temperature: &temperatureAboveRange}},
			{Name: "temperature on a reasoning model", Request: llm.TextGenerationRequest{
				Model: "fixture-reasoner-b", Temperature: &temperature,
			}},
			{Name: "reasoning effort on a model without effort", Request: llm.TextGenerationRequest{
				ReasoningEffort: llm.ReasoningEffortHigh,
			}},
			{Name: "unmapped reasoning effort", Request: llm.TextGenerationRequest{
				Model: "fixture-reasoner-b", ReasoningEffort: llm.ReasoningEffortMax,
			}},
		},
	})
}

func newFixtureQuery(t testing.TB, connection llmtest.FakeConnection) *llm.TextGenerationQuery {
	client := newFixtureClient(t, connection)
	return client.GenerateText()
}

func newFixtureClient(t testing.TB, connection llmtest.FakeConnection) *fixturellm.Client {
	t.Helper()
	client, err := fixturellm.New(fixturellm.Config{
		Model: connection.Model, Endpoint: connection.BaseURL, MaxResponseBytes: connection.MaxResponseBytes,
	}, sdkgo.StaticCredentialProvider[fixturellm.Credentials]{connection.Reference: {APIKey: connection.APIKey}})
	require.NoError(t, err)
	return client
}
```

A Chat Completions dialect also adds the optional cases: a content-policy
error when `ContentPolicyErrorToken` is set, a 2xx error object, an
interrupted stream and a complete body for a streaming request when
`IsStreaming` is set, and a request that sends no optional or sampling field.
`ErrorTokenPointers` places the error tokens where the Profile reads them, such
as `/type` and `/code` for a top-level error envelope, and the quota case
requires the Failure to name the quota token. The exchange suite runs without
Dex and records no text Stream; the real-Dex scenarios prove the streamed
text and its order.

The real-Dex scenarios use the Dex Server at `DEX_FLOW_SERVICE_ADDRESS`, or
`127.0.0.1:8801` when it is unset, and fail when that server is unreachable.
The caller's test file carries `//go:build integration`, as
[`integrationtest/llm_scenarios_integration_test.go`](integrationtest/llm_scenarios_integration_test.go)
does.

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

The `integrationtest/fixturellm` package is a lab connector built on `llm` and
`openaichat`. Its exchange test runs with the ordinary suite, and the
integration run adds the real-Dex text-generation scenarios: a generated
result streamed in order, a rate limit retried after its `Retry-After`, an
unwired optional branch that fails the Flow, a Step's model pick overriding
the connection model, a provider that stays silent past Dex's minimum
heartbeat timeout, a Worker lost mid-exchange whose call is repeated on a new
Worker, and an interrupted stream whose partial text stays on the text Stream
before the retry's text. The silent provider takes about 20 seconds.

Run it against a real Dex Server:

```bash
GOWORK=off go test -tags=integration ./integrationtest/... -count=1 -v
```
