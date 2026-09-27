# xAI Grok Connector

The Grok Connector is an independent Go module for the
[xAI API](https://docs.x.ai/overview)
[Chat Completions](https://docs.x.ai/developers/rest-api-reference/inference/chat-completions)
endpoint. It provides `grok.NewGenerateTextStep`, the provider-neutral
`generateText` Query that every lab connector shares, for Grok text or JSON
Schema structured output, reasoning effort, token usage, and streamed text.
The contract is specified in
[Text generation connectors](../../docs/connector-contract.md#text-generation-connectors).

The module lives in the `connectors/xai` company directory and its package is
named `grok`, so import it with the explicit name:

```bash
go get github.com/superdurable/dex-connectors-library/connectors/xai@v0.1.0
```

```go
grok "github.com/superdurable/dex-connectors-library/connectors/xai"
```

## Connection

The connection kind is `grok-api-key`. Its one secret field, `api_key`, is an
xAI API key from the [xAI Console](https://console.x.ai). The connector sends
it only as an `Authorization: Bearer` header to the configured xAI endpoint,
never follows a redirect, and keeps it out of Results, Failures, Receipts,
Streams, and formatted values.

Dex Web **Connections** renders the manifest form for this connection.
Applications load the local development store and create the typed
Connection once at startup, as
[`examples/summarize-text/main.go`](examples/summarize-text/main.go) does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := grok.NewLocalConnection(store, summarizetext.ConnectionName)
if err != nil {
	return err
}
```

Credentials are reread before every provider call, so a replaced key takes
effect without a restart. Configuration is captured at startup:

| Field | Default | Meaning |
| --- | --- | --- |
| `model` | `grok-4.3` | Model for every request on this connection unless the request sets `Model`. Blank uses the default. |
| `endpoint` | `https://api.x.ai/v1` | The global endpoint, or `https://us.api.x.ai/v1` for US request handling and inference. `New` rejects any other value. |
| `maxResponseBytes` | `8388608` (8 MiB) | Larger responses select `invalidResponse`; nothing is truncated silently. |

[`grok-4.3`](https://docs.x.ai/developers/models/grok-4.3) is a current text
model, listed on xAI's models and pricing pages without a preview or beta
label. It has a 1M-token context and costs $1.25 input and $2.50 output per
million tokens for prompts under 200k tokens. That sits
between Grok Build 0.1 and the $2 / $6 frontier models `grok-4.5` through
`grok-4.7` on the [pricing page](https://docs.x.ai/developers/pricing). It
reasons at `low` effort unless the request sets another.

The [US regional endpoint](https://docs.x.ai/developers/advanced-api-usage/regions)
accepts the same keys, bills tokens at 1.1 times the global rate, and
currently serves only `grok-4.7` and `grok-4.6`. Set `model` to one of them
with that endpoint; the default `grok-4.3` returns 404 `not-found` there,
which selects `providerRejected`.

`grok.WithHTTPClient` supplies a custom transport, such as a proxy. The
connector uses a copy of that client that never follows redirects.
`grok.WithBaseURLForTest` points the connection at a loopback fake provider,
such as `llmtest.FakeProvider`; `New` rejects any other host.

## Generate text

The [summarize-text Flow](examples/summarize-text/flow/workflow.go) wires the
Step and streams the summary to its text Stream:

```go
dex.DefineStep(grok.NewGenerateTextStep(grok.GenerateTextStepConfig[SummaryRequest]{
	StepType: summarizeTextStepType, ConnectionName: ConnectionName,
	Annotations: sdkgo.StepAnnotations{
		GroupID: "summary", GroupLabel: "Summary",
		Explanation: "Ask a Grok model for a short summary of the submitted text.",
	},
	ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
		ID: "summaryModel", UnitID: grok.UIUnitModelPicker, Label: "Summary model",
		Description: "Choose the Grok model that writes the summary, or keep the connection's model.",
		Bindings:    []sdkgo.ConnectorUIBinding{{Port: grok.UIModelPickerPortModel, JSONPointer: "/model"}},
	}}},
	Connection:          flow.connection,
	MapToOperationInput: flow.MapToGenerateTextRequest,
	TextStream:          &summaryTextStream,
	Generated:           sdkgo.GoTo(recordSummaryOutcome{}),
	Truncated:           sdkgo.GoTo(recordSummaryOutcome{}),
	Blocked:             sdkgo.GoTo(recordSummaryOutcome{}),
})),
```

```go
func (flow *Flow) MapToGenerateTextRequest(request SummaryRequest) grok.GenerateTextRequest {
	return grok.GenerateTextRequest{
		Model:           flow.summaryModel.Model,
		Instructions:    "Summarize the user's text in at most three sentences. Use only facts stated in the text.",
		Messages:        []llm.Message{{Role: llm.MessageRoleUser, Text: request.Text}},
		MaxOutputTokens: maxSummaryOutputTokens,
	}
}
```

`grok.GenerateTextRequest` and `grok.GenerateTextResponse` alias
`llm.TextGenerationRequest` and `llm.TextGenerationResponse`, so the same
application code runs against another lab by changing the connection. The
request maps to the documented body:

| `GenerateTextRequest` | Chat Completions |
| --- | --- |
| `Model` (empty uses the connection `model`) | `model` |
| `Instructions` | the first message, with the `system` role |
| `Messages[]` (`user` or `assistant`) | `messages[]` |
| `StructuredOutput` | `response_format` `json_schema` with `strict: true` |
| `MaxOutputTokens` | `max_completion_tokens`, which bounds visible output only, not reasoning tokens |
| `Temperature` (0 to 2; nil omits) | `temperature` |
| `ReasoningEffort` | `reasoning_effort`, for the models below |

Reasoning effort support follows the
[reasoning guide](https://docs.x.ai/developers/model-capabilities/text/reasoning)
and each model's page. Any other combination selects `defect` without a
request:

| Model | Accepted `ReasoningEffort` |
| --- | --- |
| `grok-4.3`, `grok-4.3-latest` | `none`, `low`, `medium`, `high`, `xhigh` |
| `grok-4.5`, `grok-4.5-latest`, `grok-build-latest` | `low`, `medium`, `high`; xAI would serve `xhigh` as `high` |
| `grok-4.20` and its aliases, `grok-build-0.1` and its `grok-code-fast` aliases | none; leave `ReasoningEffort` empty |
| `grok-4.7`, `grok-4.6`, and any other model | `low`, `medium`, `high`, `xhigh`; reasoning cannot be disabled |

`xhigh` is `llm.ReasoningEffortExtraHigh`; `minimal` and `max` are never
accepted.

Every request sends `stream: true` with `stream_options.include_usage`, so a
reasoning model that thinks for minutes keeps the exchange visibly alive and
the text reaches the Step's text Stream as it arrives. The reasoning text
stays private; `Usage.ReasoningTokens` reports how many tokens it used. xAI
reports `completion_tokens` without the reasoning tokens and adds both to
`total_tokens`, so the connector adds `reasoning_tokens` to
`Usage.OutputTokens`. `OutputTokens` then includes `ReasoningTokens`, as the
shared `llm.Usage` contract states for every lab, and `Usage.TotalTokens` is
the billed total. A response whose `total_tokens` equals `prompt_tokens` plus
`completion_tokens` already counts the reasoning there and is kept as
reported, so the reasoning is never counted twice.

Structured output must stay inside the portable schema subset, which every
current Grok model accepts as
[structured outputs](https://docs.x.ai/developers/model-capabilities/text/structured-outputs).
xAI enforces `minLength` and `maxLength` only up to 2,048 and `minItems` and
`maxItems` only up to 256, so the connector validates the returned text
against the application's schema and selects `invalidResponse` on a mismatch.

## Choose a model per Step

The connector ships a Connector Studio bundle with one configuration unit,
`modelPicker` (`grok.UIUnitModelPicker`, output port
`grok.UIModelPickerPortModel`). A Flow adds it to a Step's `ConfigurationUI`,
as the Step above does, and Dex Web **Connections** then shows a tab for that
Step. The tab lists models live through two pinned commands that the Dex Web
broker runs with the stored key as a bearer token; the key never reaches the
browser frame:

1. `listModels`, `GET https://api.x.ai/v1/language-models`, which carries
   each model's output modalities, aliases, and reasoning efforts;
2. `listModelsUS`, `GET https://us.api.x.ai/v1/models`, tried only when the
   first fails.

Both use the `bearer` scheme without fixed headers, so Dex Web
`cli-v0.13.8` and earlier run them too. Because the list is live, a model xAI
adds appears without a connector release. Models are shown in xAI's order,
each labeled with its aliases and described by its reasoning efforts, so
searching for an alias such as `grok-4.20-reasoning` finds its model.
Multi-agent models, which do not serve Chat Completions, and models that
output no text stay behind **Show all models**. The user can
also keep the connection's model, which saves an empty `model`, or type any
model ID. An API key whose
[endpoint ACLs](https://docs.x.ai/developers/management-api-guide) exclude the
model lists, such as one limited to `api-key:endpoint:chat`, can get 403 from
both; the picker then offers manual entry.

The picker cannot see the connection's endpoint, so it lists the global
models even for a connection on `https://us.api.x.ai/v1`. That endpoint
currently serves only `grok-4.7` and `grok-4.6`; any other pick, such as
`grok-4.3` or `grok-4.5`, returns 404 `not-found` there and selects
`providerRejected`.

The application reads the pick once at startup and passes it as the request
`Model`; restart it after saving a pick. The example reads it like this:

```go
loaded, err := localconfig.LoadOperationConfiguration[summarizetext.SummaryModelConfiguration](
	store, summarizetext.SummaryModelConfigurationRef(),
)
if errors.Is(err, localconfig.ErrConfigurationNotFound) {
	return summarizetext.SummaryModelConfiguration{}, nil
}
```

## Branches, retry, and Query semantics

`generateText` is a Query with no idempotency key: Chat Completions creates
no provider resource and accepts no idempotency key, so a repeated call only
bills the tokens again.

| Branch | Required | Selected when |
| --- | --- | --- |
| `generated` | yes | The model finished with `stop` or `end_turn`. |
| `truncated` | no | The model stopped with `length`; `Text` holds any partial output. |
| `blocked` | no | The model finished with `content_filter` or a refusal. `Text` is empty. |
| `providerRejected` | no | A conclusive rejection: 400 (including `invalid-argument` for an incorrect key), 401, 402, 403, 404 `not-found`, 422, or 501. |
| `invalidResponse` | no | The response is malformed, larger than `maxResponseBytes`, or has an unknown finish reason, a stream carries an error object, or structured output does not match its schema. |
| `defect` | no | Local input, credentials, or connection configuration is invalid. |

An unwired optional branch fails the Flow. HTTP 408, 429, and 5xx other than
501, transport failures, and a stream that disconnects before a finish reason
return Retry. A `Retry-After` header becomes the Dex retry delay, capped at
one hour. xAI errors have the shape `{"code": "not-found", "error": "..."}`;
a `Failure` carries only the status and the `code` token, never the message,
prompt, or text.

Streamed text reaches the Step's text Stream as it arrives. After a Retry the
Stream can hold the interrupted attempt's text followed by the whole text of
the next attempt; the Result's `Text` is the only authoritative text.

## Step defaults

| Option | Default |
| --- | --- |
| Execute durability | `sync` |
| Execute timeout | 900 seconds; each HTTP exchange is bounded at 870 seconds |
| Heartbeat timeout | 60 seconds; the pipeline heartbeats every 5 seconds while a call is in flight |
| Retry | 2-second initial interval, backoff 2, 60-second maximum interval, 4 attempts, 30 minutes total |

A generation usually exceeds the seven-second ASYNC local phase, and an async
fallback attempt would call xAI again, so Execute uses `sync`. xAI's
examples give reasoning requests a one-hour client timeout, but each exchange
here is cut at 870 seconds and returns Retry, so the attempt is sent again
while the 30-minute retry budget lasts and each attempt is billed.
`MaxOutputTokens` cannot prevent that, because `max_completion_tokens` bounds
visible output only. Only a lower reasoning effort, or a faster or
non-reasoning model, bounds reasoning time. Override short calls through
`StepOptionsOverride`.

Tools, image input, search, deferred completions, multi-agent models, and the
Responses API are not part of this release. xAI calls Chat Completions a
legacy endpoint and adds new features to its Responses API first; Chat
Completions is used here because it is stateless and shares the
`openaichat` wire format, so `generateText` stays a Query.

## Example

The [summarize-text example](examples/summarize-text/README.md) runs one Flow
from Dex Web **Start Flow**: it persists the request, asks Grok for a summary
while streaming it, and completes with a generated, truncated, or blocked
outcome.

## Verify

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

The real Dex suites need a running `dexcli dev`:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1
```

An opt-in live test makes one unbilled unknown-model request and one tiny
structured generation with the connection's default model at that model's
default reasoning effort, and checks that `OutputTokens` includes
`ReasoningTokens`. It reads a dedicated key from the environment and never
prints it:

```bash
XAI_CONNECTOR_TEST_API_KEY=... GOWORK=off go test -tags=live -run TestLive ./...
```

Set `XAI_CONNECTOR_TEST_MODEL` to test another model, and
`XAI_CONNECTOR_TEST_ENDPOINT=https://us.api.x.ai/v1` with `grok-4.7` or
`grok-4.6` to test the US regional endpoint. xAI rejects requests from a team
whose prepaid credits are depleted.
