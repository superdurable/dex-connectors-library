# Claude Connector

The Claude Connector is an independent Go module for the Anthropic
[Claude API](https://platform.claude.com/docs/en/api/overview)
[Messages](https://platform.claude.com/docs/en/api/messages/create) endpoint.
It provides `claude.NewGenerateTextStep`, the provider-neutral `generateText`
Query that every lab connector shares, for Claude text or JSON Schema
structured output, effort, token usage, and streamed text. The contract is
specified in
[Text generation connectors](../../docs/connector-contract.md#text-generation-connectors).

Install a published component release:

```bash
go get github.com/superdurable/dex-connectors-library/connectors/anthropic@v0.1.0
```

The module lives in the company directory, `connectors/anthropic`, and its
package is named after the connector, `claude`, so import it by that name:

```go
claude "github.com/superdurable/dex-connectors-library/connectors/anthropic"
```

## Connection

The connection kind is `claude-api-key`. Its one secret field, `api_key`, is
a Claude API key from the [Claude Console](https://platform.claude.com/settings/keys).
The connector sends it only as an `Authorization: Bearer` header, which
Claude [documents](https://platform.claude.com/docs/en/manage-claude/authentication)
for API keys, to `https://api.anthropic.com`, the one Claude API host. It
never follows a redirect and keeps the key out of Results, Failures,
Receipts, Streams, and formatted values. Every request also sends the
required `anthropic-version: 2023-06-01` header.

Dex Web **Connections** renders the manifest form for this connection.
Applications load the local development store and create the typed
Connection once at startup, as
[`examples/summarize-text/main.go`](examples/summarize-text/main.go) does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := claude.NewLocalConnection(store, summarizetext.ConnectionName)
if err != nil {
	return err
}
```

Credentials are reread before every provider call, so a replaced key takes
effect without a restart. Configuration is captured at startup:

| Field | Default | Meaning |
| --- | --- | --- |
| `model` | `claude-sonnet-5` | Model for every request on this connection unless the request sets `Model`. Blank uses the default. |
| `workspaceId` | blank | A `wrkspc_` workspace ID sent as `anthropic-workspace-id`. A multi-workspace API key requires it; a key scoped to one workspace omits it. Any other value fails `New`. |
| `defaultMaxOutputTokens` | `16000` | `max_tokens` for a request that leaves `MaxOutputTokens` zero, because Claude requires one. It includes thinking tokens and must not exceed the model's output limit. |
| `maxResponseBytes` | `8388608` (8 MiB) | Larger responses select `invalidResponse`; nothing is truncated silently. |

`claude-sonnet-5` is Claude Sonnet 5, the generally available model Claude
[describes](https://platform.claude.com/docs/en/models/sonnet-5/overview) as
the best combination of speed and intelligence, at $2 per million input
tokens and $10 per million output tokens, between Claude Haiku 4.5 and
Claude Opus 5.5 in the [current lineup](https://platform.claude.com/docs/en/about-claude/models/overview).
The `16000` default is sized so a generation that uses all of it still ends
inside one 870-second HTTP exchange. Anthropic's SDKs
[estimate](https://github.com/anthropics/anthropic-sdk-python/blob/main/src/anthropic/_base_client.py)
128,000 output tokens per hour, about 35 per second, and refuse a
non-streaming request above about 21,300 `max_tokens` as possibly
[longer than 10 minutes](https://platform.claude.com/docs/en/api/errors#long-requests).
At that rate 16000 tokens take about 7.5 minutes. The default is also below
the 64K output limit of Claude's smallest active models.

An exchange that outlasts 870 seconds does not select `truncated`: the
timeout interrupts the stream, which returns Retry and bills the tokens
again, and after four attempts or 30 minutes the Step fails without `Text`.
A request that sets `MaxOutputTokens` above about 30000, or a connection that
raises `defaultMaxOutputTokens` that far, takes that risk, and thinking tokens
count toward the limit. The exchange cannot be longer than the 900-second
Execute timeout, because `New` rejects a `WithHTTPClient` client whose
`Timeout` reaches it, so ask for long outputs in several shorter requests.

`claude.WithHTTPClient` supplies a custom transport, such as a proxy. The
connector uses a copy of that client that never follows redirects.
`claude.WithBaseURLForTest` points the connection at a loopback fake
provider, such as `llmtest.FakeProvider`; `New` rejects any other host.

## Generate text

The [summarize-text Flow](examples/summarize-text/flow/workflow.go) wires the
Step and streams the summary to its text Stream:

```go
dex.DefineStep(claude.NewGenerateTextStep(claude.GenerateTextStepConfig[SummaryRequest]{
	StepType: summarizeTextStepType, ConnectionName: ConnectionName,
	Annotations: sdkgo.StepAnnotations{
		GroupID: "summary", GroupLabel: "Summary",
		Explanation: "Ask a Claude model for a short summary of the submitted text.",
	},
	ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
		ID: "summaryModel", UnitID: claude.UIUnitModelPicker, Label: "Summary model",
		Description: "Choose the Claude model that writes the summary, or keep the connection's model.",
		Bindings:    []sdkgo.ConnectorUIBinding{{Port: claude.UIModelPickerPortModel, JSONPointer: "/model"}},
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
func (flow *Flow) MapToGenerateTextRequest(request SummaryRequest) claude.GenerateTextRequest {
	return claude.GenerateTextRequest{
		Model:           flow.summaryModel.Model,
		Instructions:    "Summarize the user's text in at most three sentences. Use only facts stated in the text.",
		Messages:        []llm.Message{{Role: llm.MessageRoleUser, Text: request.Text}},
		MaxOutputTokens: maxSummaryOutputTokens,
	}
}
```

`claude.GenerateTextRequest` and `claude.GenerateTextResponse` alias
`llm.TextGenerationRequest` and `llm.TextGenerationResponse`, so the same
application code runs against another lab by changing the connection. The
request maps to the documented `POST /v1/messages` body, which always sets
`stream: true`:

| `GenerateTextRequest` | Messages API |
| --- | --- |
| `Model` (empty uses the connection `model`) | `model` |
| `Instructions` | `system` |
| `Messages[]` (`user` or `assistant`) | `messages[]` with string `content` |
| `StructuredOutput` | `output_config.format` with `type: json_schema` and the schema; `Description` becomes the schema's root `description` unless it has one, and `Name` is not sent |
| `MaxOutputTokens` (zero uses `defaultMaxOutputTokens`) | `max_tokens`, which includes thinking tokens |
| `Temperature` (nil omits) | `temperature` |
| `ReasoningEffort` | `output_config.effort`: `low`, `medium`, `high`, `xhigh` for `ReasoningEffortExtraHigh`, or `max` |

Claude's models accept different values, so the connector checks them per
model and selects `defect` without a request for any other value, following
Claude's [effort](https://platform.claude.com/docs/en/build-with-claude/effort)
and [sampling](https://platform.claude.com/docs/en/api/messages/create) rules:

| Models | `ReasoningEffort` | `Temperature` |
| --- | --- | --- |
| Claude Fable 5.1, Mythos 5.1, Fable 5, Mythos 5, Opus 5.5, Opus 5, Opus 4.8, Opus 4.7, Sonnet 5, and model IDs this release does not name | `low`, `medium`, `high`, `xhigh`, `max` | only `1.0`, the default |
| Claude Opus 4.6, Sonnet 4.6 | `low`, `medium`, `high`, `max` | `0` to `1` |
| Claude Mythos Preview | `low`, `medium`, `high`, `max` | only `1.0` |
| Claude Opus 4.5 | `low`, `medium`, `high` | `0` to `1` |
| Claude Sonnet 4.5, Haiku 4.5 | none | `0` to `1` |

Claude has no `none` or `minimal` effort. Because a model Claude adds later
uses the first row, a new model with narrower support is rejected by Claude
with HTTP 400, which selects `providerRejected`. The example leaves
`ReasoningEffort` unset, so any model the picker lists accepts its request.

The connector sends no `thinking` setting, so each model's default applies.
Thinking text never reaches `Text` or the text Stream, and
`Usage.ReasoningTokens` reports `output_tokens_details.thinking_tokens` when
Claude sends it. `Usage.InputTokens` adds cache writes and cache reads to
`input_tokens`, as Claude's total input does, and `CachedInputTokens` is the
cache reads.

Structured output must stay inside the portable schema subset. Claude's JSON
outputs reject numeric, string, and most array bounds, so the connector moves
`minimum`, `maximum`, `minLength`, `maxLength`, `minItems`, and `maxItems`
into the node's `description` and still validates the returned text against
the application's original schema, selecting `invalidResponse` on a mismatch.
See [JSON Schema limitations](https://platform.claude.com/docs/en/build-with-claude/structured-outputs#json-schema-limitations).

## Choose a model per Step

The connector ships a Connector Studio bundle with one configuration unit,
`modelPicker` (`claude.UIUnitModelPicker`, output port
`claude.UIModelPickerPortModel`). A Flow adds it to a Step's
`ConfigurationUI`, as the Step above does, and Dex Web **Connections** then
shows a tab for that Step. The tab lists models live through the bundle's
`listModels` command, `GET https://api.anthropic.com/v1/models?limit=1000`
with the fixed `anthropic-version: 2023-06-01` header, which the Dex Web
broker runs with the stored key as a bearer token; the key never reaches the
browser frame. The bundle follows `has_more` and `last_id` through the
command's `afterId` parameter, which Dex sends as `after_id`. Because the list
is live, a model Claude adds appears without a connector release.

Every listed model serves the Messages API, so none is hidden. Each shows its
`display_name`, its context window and output limit, and badges for the
structured output, effort, and thinking `capabilities` Claude reports, in
Claude's order, newest first. The user can also keep the connection's model,
which saves an empty `model`, or type any model ID when listing fails.

Dex Web `cli-v0.13.8` and earlier ignore `fixedHeaders`, so they send the
list without `anthropic-version`, Claude rejects it, and the picker offers
manual model ID entry. The command sends no `anthropic-workspace-id`, so a
multi-workspace API key also falls back to manual entry; the connection's
`workspaceId` still applies to generation.

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

`generateText` is a Query with no idempotency key: the Messages API stores no
provider resource and accepts no idempotency key, so a repeated call only
bills the tokens again.

| Branch | Required | Selected when |
| --- | --- | --- |
| `generated` | yes | The model finished with `end_turn` or `stop_sequence`. |
| `truncated` | no | The model stopped with `max_tokens` or `model_context_window_exceeded`; `Text` holds any partial output. |
| `blocked` | no | The model finished with `refusal`, Claude's safety classifier stop. `Text` is empty. |
| `providerRejected` | no | A conclusive rejection: 400 `invalid_request_error`, including a spend limit you set; 401 `authentication_error`; 402 `billing_error`; 403 `permission_error`; 404 `not_found_error`, such as an unknown model; 413 `request_too_large`; 501; or the tier spend-cap 429 whose `error.details.error_code` is `enforced_spend_limit_reached`. |
| `invalidResponse` | no | The response is malformed, larger than `maxResponseBytes`, or has an unknown stop reason such as `tool_use`, or structured output does not match its schema. |
| `defect` | no | Local input, credentials, or connection configuration is invalid. |

A 402 or the spend-cap 429 carries `QUOTA_EXHAUSTED`, because retrying cannot
restore access. An unwired optional branch fails the Flow. HTTP 408, 429, and
5xx other than 501, including 529 `overloaded_error`, transport failures, and
a stream that ends before `message_stop` return Retry. An error object after
a 200 status, either a mid-stream `error` event, as the
[streaming guide](https://platform.claude.com/docs/en/build-with-claude/streaming#error-events)
describes, or a complete JSON error body from a gateway that ignores
streaming, follows the same rules without a status. The
`enforced_spend_limit_reached` code or a `billing_error` type selects
`providerRejected` with `QUOTA_EXHAUSTED`; an `overloaded_error`,
`api_error`, `timeout_error`, or `rate_limit_error` type returns Retry; and
any other error object selects `invalidResponse`. A
`retry-after` header becomes the Dex retry delay, capped at one hour. A
`Failure` carries only the status and Claude's error type and code, never the
message, prompt, or text. The Receipt carries the `request-id` header and
copies Claude's `anthropic-ratelimit-{requests,tokens,input-tokens,output-tokens}-{limit,remaining}`
headers.

Streamed text reaches the Step's text Stream as it arrives. After a Retry the
Stream can hold the interrupted attempt's text followed by the whole text of
the next attempt, and a response that ends in `refusal` can leave text there;
the Result's `Text` is the only authoritative text.

## Step defaults

| Option | Default |
| --- | --- |
| Execute durability | `sync` |
| Execute timeout | 900 seconds; each HTTP exchange is bounded at 870 seconds |
| Heartbeat timeout | 60 seconds; the pipeline heartbeats every 5 seconds while a call is in flight |
| Retry | 2-second initial interval, backoff 2, 60-second maximum interval, 4 attempts, 30 minutes total |

A generation usually exceeds the seven-second ASYNC local phase, and an async
fallback attempt would call Claude again, so Execute uses `sync`. Claude
[recommends streaming](https://platform.claude.com/docs/en/api/errors#long-requests)
for long requests, so every request streams and the operation uses the
streaming budget. Override short calls through `StepOptionsOverride`.

Tools, images, documents, prompt caching controls, the `thinking` and
`stop_sequences` parameters, and the Batches API are not part of this
release.

## Example

The [summarize-text example](examples/summarize-text/README.md) runs one Flow
from Dex Web **Start Flow**: it persists the request, asks Claude for a
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
CLAUDE_CONNECTOR_TEST_API_KEY=... GOWORK=off go test -tags=live -run TestLive ./...
```

Set `CLAUDE_CONNECTOR_TEST_MODEL` to test another model and
`CLAUDE_CONNECTOR_TEST_WORKSPACE_ID` for a multi-workspace key. A billing
issue returns HTTP 402 and a reached tier spend cap returns 429
`enforced_spend_limit_reached`; both select `providerRejected` with
`QUOTA_EXHAUSTED`.
