# Kimi API Connector

The Kimi Connector is an independent Go module for Moonshot AI's
[Kimi API](https://platform.kimi.ai/docs/api/overview)
[Chat Completions](https://platform.kimi.ai/docs/api/chat) endpoint. It
provides `kimi.NewGenerateTextStep`, the provider-neutral `generateText`
Query that every lab connector shares, for Kimi K3 and K2 text or JSON Schema
structured output, K3 reasoning effort, token usage, and streamed text. The
contract is specified in
[Text generation connectors](../../../docs/connector-contract.md#text-generation-connectors).

Install a published component release:

```bash
go get github.com/superdurable/dex-connectors-library/connectors/moonshot/kimi@v0.1.0
```

The module path ends in `moonshot/kimi`, and its package name is `kimi`.

## Connection

The connection kind is `kimi-api-key`. Its one secret field, `api_key`, is a
Kimi API key from the Kimi API Platform console. The connector sends it only
as an `Authorization: Bearer` header to the configured endpoint, never
follows a redirect, and keeps it out of Results, Failures, Receipts, Streams,
and formatted values.

Kimi runs two platforms whose accounts, balances, and keys are separate: a
key from one returns 401 on the other. The `endpoint` field names the
platform, and `New` accepts only these two values, with an optional trailing
slash:

| Platform | Console | `endpoint` |
| --- | --- | --- |
| Global | `platform.kimi.ai` | `https://api.moonshot.ai/v1` (default) |
| China | `platform.kimi.com` | `https://api.moonshot.cn/v1` |

Dex Web **Connections** renders the manifest form for this connection.
Applications load the local development store and create the typed
Connection once at startup, as
[`examples/summarize-text/main.go`](examples/summarize-text/main.go) does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := kimi.NewLocalConnection(store, summarizetext.ConnectionName)
if err != nil {
	return err
}
```

Credentials are reread before every provider call, so a replaced key takes
effect without a restart. Configuration is captured at startup:

| Field | Default | Meaning |
| --- | --- | --- |
| `model` | `kimi-k2.6` | Model for every request on this connection unless the request sets `Model`. Blank uses the default. |
| `endpoint` | `https://api.moonshot.ai/v1` | The Kimi platform that issued the key; see the table above. Blank uses the default. |
| `maxResponseBytes` | `8388608` (8 MiB) | Larger responses select `invalidResponse`; nothing is truncated silently. |

`kimi-k2.6` is Kimi's general-purpose [model](https://platform.kimi.ai/docs/models)
with a 256K-token context, priced below the flagship `kimi-k3` on the
[pricing page](https://platform.kimi.ai/docs/pricing/chat). It thinks by
default, and its thinking tokens count toward `MaxOutputTokens`.

`kimi.WithHTTPClient` supplies a custom transport, such as a proxy. The
connector uses a copy of that client that never follows redirects.
`kimi.WithBaseURLForTest` points the connection at a loopback fake provider,
such as `llmtest.FakeProvider`; `New` rejects any other host.

## Generate text

The [summarize-text Flow](examples/summarize-text/flow/workflow.go) wires the
Step and streams the summary to its text Stream:

```go
dex.DefineStep(kimi.NewGenerateTextStep(kimi.GenerateTextStepConfig[SummaryRequest]{
	StepType: summarizeTextStepType, ConnectionName: ConnectionName,
	Annotations: sdkgo.StepAnnotations{
		GroupID: "summary", GroupLabel: "Summary",
		Explanation: "Ask a Kimi model for a short summary of the submitted text.",
	},
	ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
		ID: "summaryModel", UnitID: kimi.UIUnitModelPicker, Label: "Summary model",
		Description: "Choose the Kimi model that writes the summary, or keep the connection's model.",
		Bindings:    []sdkgo.ConnectorUIBinding{{Port: kimi.UIModelPickerPortModel, JSONPointer: "/model"}},
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
func (flow *Flow) MapToGenerateTextRequest(request SummaryRequest) kimi.GenerateTextRequest {
	return kimi.GenerateTextRequest{
		Model:           flow.summaryModel.Model,
		Instructions:    "Summarize the user's text in at most three sentences. Use only facts stated in the text.",
		Messages:        []llm.Message{{Role: llm.MessageRoleUser, Text: request.Text}},
		MaxOutputTokens: maxSummaryOutputTokens,
	}
}
```

`kimi.GenerateTextRequest` and `kimi.GenerateTextResponse` alias
`llm.TextGenerationRequest` and `llm.TextGenerationResponse`, so the same
application code runs against another lab by changing the connection. The
request maps to the documented body:

| `GenerateTextRequest` | Chat Completions |
| --- | --- |
| `Model` (empty uses the connection `model`) | `model` |
| `Instructions` | the first message, with the `system` role |
| `Messages[]` (`user` or `assistant`) | `messages[]`, text only; see assistant turns below |
| `StructuredOutput` | `response_format` `json_schema` with `strict: true` |
| `MaxOutputTokens` | `max_completion_tokens`, which also counts thinking tokens; unset on `kimi-k3` means 131072 |
| `Temperature` | never sent; any value selects `defect` |
| `ReasoningEffort` | `reasoning_effort` on `kimi-k3` models only: `low`, `high`, or `max` |

Every request sends `stream: true` with `stream_options.include_usage`,
because Kimi's gateway ends a non-streaming request that stays silent for 900
seconds with HTTP 504. Kimi fixes `temperature`, `top_p`, `n`, and the
penalties for every current model and rejects any other value, so a
`Temperature`, even `1.0`, selects `defect` without a request. Kimi K3 always
reasons and accepts only `low`, `high`, and `max`; K2 models control
reasoning with a `thinking` switch this connector does not send, so any
`ReasoningEffort` on them selects `defect`. The reasoning text stays private,
and Kimi reports no separate reasoning token count, so
`Usage.ReasoningTokens` is zero. `Usage.CachedInputTokens` is Kimi's
`prompt_tokens_details.cached_tokens`.

An assistant turn carries only its text, so it reaches Kimi without the
`reasoning_content` Kimi returned for it. Kimi's
[thinking guide](https://platform.kimi.ai/docs/guide/use-thinking-models)
requires that field on every earlier assistant message for `kimi-k3` and
`kimi-k2.7-code`, whose Preserved Thinking is always on, and does not say
what happens without it; this connector has not tested it either. Assistant
turns in `Messages[]` are therefore used as Kimi documents only on
`kimi-k2.6`, whose `thinking.keep` defaults to ignoring earlier reasoning. On
the other two models, send one user turn and put any earlier conversation in
its text or in `Instructions`.

Structured output must stay inside the portable schema subset. Kimi's strict
mode requires [Moonshot Flavored JSON Schema](https://github.com/MoonshotAI/walle/blob/main/docs/mfjs-spec.md)
(MFJS), which does not support `title` or `format` and rejects any
constraint keyword beside a nullable type such as `["string", "null"]`. The
connector therefore removes `title` and moves `minimum`, `maximum`,
`minLength`, `maxLength`, `minItems`, `maxItems`, and `format` into the
node's description, so an optional bounded value still reaches Kimi. It
validates the returned text against the application's original schema, strips
a Markdown fence, and selects `invalidResponse` on a mismatch. MFJS also
rejects `properties` or `items` beside a nullable type, so Kimi can reject a
nullable object or array, which selects `providerRejected`; prefer a
non-nullable object or array with nullable leaves.

Kimi's [structured output guide](https://platform.kimi.ai/docs/guide/response_format)
says the default `kimi-k2.6` sometimes behaves unstably with complex schemas
and hits MFJS limits more often, and it recommends `kimi-k2.7-code`, with
`kimi-k3` also reliable. On `kimi-k2.6`, a complex schema is more likely to
select `invalidResponse` or `providerRejected`, so keep schemas flat there,
or set `Model` to `kimi-k2.7-code` or `kimi-k3` for structured output.

## Choose a model per Step

The connector ships a Connector Studio bundle with one configuration unit,
`modelPicker` (`kimi.UIUnitModelPicker`, output port
`kimi.UIModelPickerPortModel`). A Flow adds it to a Step's `ConfigurationUI`,
as the Step above does, and Dex Web **Connections** then shows a tab for that
Step. The tab lists models live through two bundle commands that the Dex Web
broker runs with the stored key as a bearer token, so the key never reaches
the browser frame: `listModels`, `GET https://api.moonshot.ai/v1/models`,
and then, when the global platform rejects the key, `listModelsChina`,
`GET https://api.moonshot.cn/v1/models`. Because the list is live, a model
Kimi adds appears without a connector release.

Each model shows its context window and a reasoning badge from Kimi's
`context_length` and `supports_reasoning` flags. Retired families, such as
`moonshot-v1`, `kimi-k2-*`, and `kimi-k2.5`, stay behind
**Show all models**. The user can also keep the connection's model, which
saves an empty `model`, or type any model ID when listing fails.

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
| `generated` | yes | The model finished with `stop`. |
| `truncated` | no | The model stopped with `length`; `Text` holds any partial output. |
| `blocked` | no | Kimi returned `content_filter`, as a 400 or inside the stream, or the model finished with `content_filter` or a refusal. `Text` is empty. |
| `providerRejected` | no | A conclusive rejection: 400 `invalid_request_error`, 401 (including a key from the other platform), 403, 404 `resource_not_found_error`, 429 `exceeded_current_quota_error`, or 501. |
| `invalidResponse` | no | The response is malformed, larger than `maxResponseBytes`, or has an unknown finish reason, or structured output does not match its schema. |
| `defect` | no | Local input, credentials, or connection configuration is invalid. |

An unwired optional branch fails the Flow. Kimi's 429 carries three error
types, and only the type tells them apart: `exceeded_current_quota_error`
(an exhausted balance or token quota) selects `providerRejected` with
`QUOTA_EXHAUSTED` and is never retried, `engine_overloaded_error` returns
Retry as an availability failure, and `rate_limit_reached_error` returns
Retry as a rate limit. Other 408, 429, and 5xx responses except 501,
transport failures, and a stream that disconnects before a finish reason also
return Retry. A stream chunk whose `error.type` is one of those types
classifies the same way, and so do `server_error`, `unexpected_output`, and
`server_unavailable`; any other stream error object, or a `[DONE]` before a
finish reason, selects `invalidResponse`. A `Retry-After` header becomes the
Dex retry delay, capped at one hour. A `Failure` carries only the status and
Kimi's error type, never the message, prompt, or text.

Kimi also [returns](https://platform.kimi.ai/docs/api/errors)
`rate_limit_reached_error` when the organization reaches its tokens-per-day
limit, which resets the next day. Only the error message separates that limit
from the concurrency, per-minute request, and per-minute token limits, and
the connector never reads the message. A daily-limit 429 therefore retries
until the retry policy runs out and then fails the Step. It does not select
`providerRejected`, so an application cannot branch on it.

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
fallback attempt would call Kimi again, so Execute uses `sync`. Override short
calls through `StepOptionsOverride`.

Set `MaxOutputTokens` on every `kimi-k3` request, and a lower
`ReasoningEffort` when the task allows it. Kimi K3
[defaults](https://platform.kimi.ai/docs/api/chat) to 131072 output tokens at
`max` effort, and a generation that long can outlast the 870-second exchange
bound. A cut-off stream returns Retry, and each of the four attempts sends the
prompt again and is billed again. A longer Execute timeout through
`StepOptionsOverride` does not help, because the exchange bound stays fixed:
`New` rejects a `WithHTTPClient` client whose timeout is 900 seconds or more.

Tools, image and video input, the K2 `thinking` switch, Partial Mode, context
cache options, and the Responses and Anthropic Messages endpoints are not part
of this release.

## Example

The [summarize-text example](examples/summarize-text/README.md) runs one Flow
from Dex Web **Start Flow**: it persists the request, asks a Kimi model for a
summary while streaming it, and completes with a generated, truncated, or
blocked outcome.

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
structured generation with the connection's default model. It reads a
dedicated key from the environment and never prints it:

```bash
KIMI_CONNECTOR_TEST_API_KEY=... GOWORK=off go test -tags=live -run TestLive ./...
```

Set `KIMI_CONNECTOR_TEST_ENDPOINT=https://api.moonshot.cn/v1` for a China
platform key and `KIMI_CONNECTOR_TEST_MODEL` to test another model. An
exhausted balance returns 429 `exceeded_current_quota_error`, which the
connector classifies as `providerRejected` with `QUOTA_EXHAUSTED`.
