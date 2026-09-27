# Meta Model API Connector

The Meta Connector is an independent Go module for the
[Meta Model API](https://dev.meta.ai/docs/overview)
[Chat Completions](https://dev.meta.ai/docs/protocols/chat-completions)
endpoint. It provides `meta.NewGenerateTextStep`, the provider-neutral
`generateText` Query that every lab connector shares, for Muse Spark text or
JSON Schema structured output, reasoning effort, token usage, and streamed
text. The contract is specified in
[Text generation connectors](../../docs/connector-contract.md#text-generation-connectors).

Install a published component release:

```bash
go get github.com/superdurable/dex-connectors-library/connectors/meta@v0.1.0
```

## Connection

The connection kind is `meta-api-key`. Its one secret field, `api_key`, is a
Meta Model API key from the Model API dashboard, such as `LLM|<id>|<secret>`.
The connector sends it only as an `Authorization: Bearer` header to
`https://api.meta.ai`, the one Meta Model API host, never follows a redirect,
and keeps it out of Results, Failures, Receipts, Streams, and formatted
values.

Dex Web **Connections** renders the manifest form for this connection.
Applications load the local development store and create the typed
Connection once at startup, as
[`examples/summarize-text/main.go`](examples/summarize-text/main.go) does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := meta.NewLocalConnection(store, summarizetext.ConnectionName)
if err != nil {
	return err
}
```

Credentials are reread before every provider call, so a replaced key takes
effect without a restart. Configuration is captured at startup:

| Field | Default | Meaning |
| --- | --- | --- |
| `model` | `muse-spark-1.3` | Model for every request on this connection unless the request sets `Model`. Blank uses the default. |
| `maxResponseBytes` | `8388608` (8 MiB) | Larger responses select `invalidResponse`; nothing is truncated silently. |

`muse-spark-1.3` is the Standard-tier model that Meta
[recommends for new work](https://dev.meta.ai/docs/models); all Standard-tier
Muse Spark versions share one [price](https://dev.meta.ai/docs/pricing-rate-limits).
The `-contributor` variants cost less because Meta may train on their prompts
and completions.

`meta.WithHTTPClient` supplies a custom transport, such as a proxy. The
connector uses a copy of that client that never follows redirects.
`meta.WithBaseURLForTest` points the connection at a loopback fake provider,
such as `llmtest.FakeProvider`; `New` rejects any other host.

## Generate text

The [summarize-text Flow](examples/summarize-text/flow/workflow.go) wires the
Step and streams the summary to its text Stream:

```go
dex.DefineStep(meta.NewGenerateTextStep(meta.GenerateTextStepConfig[SummaryRequest]{
	StepType: summarizeTextStepType, ConnectionName: ConnectionName,
	Annotations: sdkgo.StepAnnotations{
		GroupID: "summary", GroupLabel: "Summary",
		Explanation: "Ask a Muse Spark model for a short summary of the submitted text.",
	},
	ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
		ID: "summaryModel", UnitID: meta.UIUnitModelPicker, Label: "Summary model",
		Description: "Choose the Meta model that writes the summary, or keep the connection's model.",
		Bindings:    []sdkgo.ConnectorUIBinding{{Port: meta.UIModelPickerPortModel, JSONPointer: "/model"}},
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
func (flow *Flow) MapToGenerateTextRequest(request SummaryRequest) meta.GenerateTextRequest {
	return meta.GenerateTextRequest{
		Model:           flow.summaryModel.Model,
		Instructions:    "Summarize the user's text in at most three sentences. Use only facts stated in the text.",
		Messages:        []llm.Message{{Role: llm.MessageRoleUser, Text: request.Text}},
		MaxOutputTokens: maxSummaryOutputTokens,
		ReasoningEffort: llm.ReasoningEffortLow,
	}
}
```

`meta.GenerateTextRequest` and `meta.GenerateTextResponse` alias
`llm.TextGenerationRequest` and `llm.TextGenerationResponse`, so the same
application code runs against another lab by changing the connection. The
request maps to the documented body:

| `GenerateTextRequest` | Chat Completions |
| --- | --- |
| `Model` (empty uses the connection `model`) | `model` |
| `Instructions` | the first message, with the `developer` role Meta gives the highest precedence |
| `Messages[]` (`user` or `assistant`) | `messages[]` |
| `StructuredOutput` | `response_format` `json_schema` with `strict: true` |
| `MaxOutputTokens` | `max_completion_tokens`, which also counts reasoning tokens |
| `Temperature` (0 to 2; nil omits) | `temperature` |
| `ReasoningEffort` | `reasoning_effort`: `minimal`, `low`, `medium`, `high`, or `xhigh` for `ReasoningEffortExtraHigh`; `max` only on `muse-spark-1.3` |

Every request sends `stream: true` with `stream_options.include_usage`,
because Meta ends a long non-streaming request with HTTP 504 and exempts
streams. Muse Spark always reasons: `ReasoningEffortNone`, `max` on another
model, or a temperature outside 0 to 2 selects `defect` without a request.
Meta recommends leaving `Temperature` nil. The reasoning text stays private;
`Usage.ReasoningTokens` reports how many tokens it used.

Structured output must stay inside the portable schema subset, which already
satisfies Meta's strict subset. The connector validates the returned text
against the application's schema and selects `invalidResponse` on a mismatch.

## Choose a model per Step

The connector ships a Connector Studio bundle with one configuration unit,
`modelPicker` (`meta.UIUnitModelPicker`, output port
`meta.UIModelPickerPortModel`). A Flow adds it to a Step's `ConfigurationUI`,
as the Step above does, and Dex Web **Connections** then shows a tab for that
Step. The tab lists models live through the bundle's `listModels` command,
`GET https://api.meta.ai/v1/models`, which the Dex Web broker runs with the
stored key as a bearer token; the key never reaches the browser frame.
Because the list is live, a model Meta adds appears without a connector
release.

Muse Spark models are shown, and Contributor variants carry a note that Meta
may train on their data. Muse Image, Muse Voice Transcribe, and SAM do not
serve Chat Completions, so they stay behind **Show all models**. The user can
also keep the connection's model, which saves an empty `model`, or type any
model ID when listing fails.

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
| `blocked` | no | The model finished with `content_filter` or a refusal, or Meta returned 400 `content_policy_violation`. `Text` is empty. |
| `providerRejected` | no | A conclusive rejection: 400, 401 `invalid_api_key`, 402 billing issue, 403, 404 `model_not_found`, or 501. |
| `invalidResponse` | no | The response is malformed, larger than `maxResponseBytes`, or has an unknown finish reason, or structured output does not match its schema. |
| `defect` | no | Local input, credentials, or connection configuration is invalid. |

An unwired optional branch fails the Flow. HTTP 408, 429, and 5xx other than
501, transport failures, and a stream that disconnects before a finish reason
return Retry. So does a stream chunk whose `error` object has a `type` or `code` Meta
documents as retryable: `server_error`, `rate_limit_error`,
`server_shutting_down`, `service_overloaded`, `backend_unavailable`, or
`rate_limit_exceeded`, as during a Meta deploy. Any other `error` object in a
stream, or a `[DONE]` before a finish reason, selects `invalidResponse`.
A `Retry-After` header becomes the Dex retry delay, capped at one
hour. A `Failure` carries only the status and Meta's error tokens, never the
message, prompt, or text. The Receipt copies Meta's
`x-ratelimit-{limit,remaining}-{requests,tokens}` headers.

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
fallback attempt would call Meta again, so Execute uses `sync`. Override short
calls through `StepOptionsOverride`.

Tools, multimodal input, search grounding, and the Responses and Messages
endpoints are not part of this release.

## Example

The [summarize-text example](examples/summarize-text/README.md) runs one Flow
from Dex Web **Start Flow**: it persists the request, asks Muse Spark for a
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
META_CONNECTOR_TEST_API_KEY=... GOWORK=off go test -tags=live -run TestLive ./...
```

Set `META_CONNECTOR_TEST_MODEL` to test another model. A billing issue
returns HTTP 402, which the connector classifies as `providerRejected` with
`QUOTA_EXHAUSTED`.
