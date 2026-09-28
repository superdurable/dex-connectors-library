# DeepSeek Connector

The DeepSeek Connector is an independent Go module for the
[DeepSeek API](https://api-docs.deepseek.com/)
[Chat Completions](https://api-docs.deepseek.com/api/create-chat-completion)
endpoint. It provides `deepseek.NewGenerateTextStep`, the provider-neutral
`generateText` Query that every lab connector shares, for text or JSON Output
structured output, thinking effort, token usage, and streamed text. The
contract is specified in
[Text generation connectors](../../docs/connector-contract.md#text-generation-connectors).

Install a published component release:

```bash
go get github.com/superdurable/dex-connectors-library/connectors/deepseek@v0.1.0
```

## Connection

The connection kind is `deepseek-api-key`. Its one secret field, `api_key`, is
a DeepSeek API key from the [DeepSeek Platform](https://platform.deepseek.com/api_keys).
The connector sends it only as an `Authorization: Bearer` header to
`https://api.deepseek.com`, the one DeepSeek API host, never follows a
redirect, and keeps it out of Results, Failures, Receipts, Streams, and
formatted values. DeepSeek echoes the key's last four characters in its 401
message; a Failure never carries provider message text.

Dex Web **Connections** renders the manifest form for this connection.
Applications load the local development store and create the typed
Connection once at startup, as
[`examples/summarize-text/main.go`](examples/summarize-text/main.go) does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := deepseek.NewLocalConnection(store, summarizetext.ConnectionName)
if err != nil {
	return err
}
```

Credentials are reread before every provider call, so a replaced key takes
effect without a restart. Configuration is captured at startup:

| Field | Default | Meaning |
| --- | --- | --- |
| `model` | `deepseek-flash` | Model for every request on this connection unless the request sets `Model`. Blank uses the default. |
| `maxResponseBytes` | `67108864` (64 MiB) | Larger responses select `invalidResponse`; nothing is truncated silently. |

`deepseek-flash` serves DeepSeek-V4.1-Flash, the model every DeepSeek API
example uses. It is the lower-priced of DeepSeek's two
[models](https://api-docs.deepseek.com/quick_start/pricing) and has a
concurrency limit five times that of `deepseek-v4-pro`. Both have a 1M-token
context and accept up to 384K output tokens.

The response limit is larger than other lab connectors' 8 MiB because
DeepSeek streams each token, reasoning included, in its own chunk of about
250 bytes. The 128K-token default output of `max` effort therefore streams
more than 30 MiB. Keep-alive comments count toward the limit too.

`deepseek.WithHTTPClient` supplies a custom transport, such as a proxy. The
connector uses a copy of that client that never follows redirects. A client
`Timeout` must stay below the 1200-second Execute timeout, or `New` returns an
error; an unset `Timeout` becomes 1170 seconds.
`deepseek.WithBaseURLForTest` points the connection at a loopback fake
provider, such as `llmtest.FakeProvider`; `New` rejects any other host.

## Generate text

The [summarize-text Flow](examples/summarize-text/flow/workflow.go) wires the
Step and streams the summary to its text Stream:

```go
dex.DefineStep(deepseek.NewGenerateTextStep(deepseek.GenerateTextStepConfig[SummaryRequest]{
	StepType: summarizeTextStepType, ConnectionName: ConnectionName,
	Annotations: sdkgo.StepAnnotations{
		GroupID: "summary", GroupLabel: "Summary",
		Explanation: "Ask a DeepSeek model for a short summary of the submitted text.",
	},
	ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
		ID: "summaryModel", UnitID: deepseek.UIUnitModelPicker, Label: "Summary model",
		Description: "Choose the DeepSeek model that writes the summary, or keep the connection's model.",
		Bindings:    []sdkgo.ConnectorUIBinding{{Port: deepseek.UIModelPickerPortModel, JSONPointer: "/model"}},
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
func (flow *Flow) MapToGenerateTextRequest(request SummaryRequest) deepseek.GenerateTextRequest {
	return deepseek.GenerateTextRequest{
		Model:           flow.summaryModel.Model,
		Instructions:    "Summarize the user's text in at most three sentences. Use only facts stated in the text.",
		Messages:        []llm.Message{{Role: llm.MessageRoleUser, Text: request.Text}},
		MaxOutputTokens: maxSummaryOutputTokens,
		ReasoningEffort: llm.ReasoningEffortNone,
	}
}
```

`deepseek.GenerateTextRequest` and `deepseek.GenerateTextResponse` alias
`llm.TextGenerationRequest` and `llm.TextGenerationResponse`, so the same
application code runs against another lab by changing the connection. The
request maps to the documented body:

| `GenerateTextRequest` | Chat Completions |
| --- | --- |
| `Model` (empty uses the connection `model`) | `model` |
| `Instructions` | the first message, with the `system` role |
| `Messages[]` (`user` or `assistant`) | `messages[]` |
| `StructuredOutput` | `response_format` `json_object`, with the schema appended to the `system` message |
| `MaxOutputTokens` | `max_tokens`, 1 to 393216; unset uses DeepSeek's 8K without thinking, 64K with thinking, or 128K at `max` effort |
| `Temperature` | never sent; any value selects `defect` |
| `ReasoningEffort` | `reasoning_effort`: `none` turns thinking off; `low`, `high`, or `max` set the thinking effort |

Every request sends `stream: true` without `stream_options`, because DeepSeek
puts the usage on the last content chunk either way. Thinking mode is on by
default at `high` effort. Its `reasoning_content` never reaches the text or
the Stream; `Usage.ReasoningTokens` reports how many tokens it used.

DeepSeek accepts `minimal`, `medium`, and `xhigh` but silently runs them as
`low`, `high`, and `high`, so those efforts select `defect` without a request.
Thinking mode also silently ignores `temperature`. A Profile cannot accept a
temperature only when `ReasoningEffort` is `none`, so the connector rejects
every temperature instead of letting DeepSeek ignore one.

DeepSeek's `response_format` accepts only `text` and `json_object`, not a
JSON Schema. The connector appends an instruction to the `system` message
that asks for one JSON object and includes the schema, which also satisfies
DeepSeek's rule that the prompt must mention JSON. Structured output must stay
inside the portable schema subset. The connector validates the returned text
against the application's schema and selects `invalidResponse` on a mismatch.
DeepSeek documents that JSON Output occasionally returns empty content, which
also selects `invalidResponse`.

## Choose a model per Step

The connector ships a Connector Studio bundle with one configuration unit,
`modelPicker` (`deepseek.UIUnitModelPicker`, output port
`deepseek.UIModelPickerPortModel`). A Flow adds it to a Step's
`ConfigurationUI`, as the Step above does, and Dex Web **Connections** then
shows a tab for that Step. The tab lists models live through the bundle's
`listModels` command, `GET https://api.deepseek.com/models`, which the Dex Web
broker runs with the stored key as a bearer token; the key never reaches the
browser frame. Because the list is live, a model DeepSeek adds appears without
a connector release.

Models are shown in DeepSeek's order with their display name, such as
`DeepSeek-V4.1-Flash`, and their context window and output limit. A model
whose `output_modalities` omit `text` cannot serve `generateText`, so it stays
behind **Show all models**. The user can also keep the connection's model,
which saves an empty `model`, or type any model ID when listing fails.

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
| `blocked` | no | The model finished with `content_filter`. `Text` is empty. |
| `providerRejected` | no | A conclusive rejection: 400 invalid format, 401 authentication fails, 402 insufficient balance (`QUOTA_EXHAUSTED`), 422 invalid parameters, another 4xx, or 501. |
| `invalidResponse` | no | The response is malformed, larger than `maxResponseBytes`, or empty, structured output does not match its schema, or the finish reason is unknown, including DeepSeek's `insufficient_system_resource` and `aborted`. |
| `defect` | no | Local input, credentials, or connection configuration is invalid. |

An unwired optional branch fails the Flow. HTTP 408, 429 (the account's
concurrency limit), 500, 503, and other 5xx except 501, transport failures,
and a stream that disconnects before a finish reason return Retry. A
`Retry-After` header becomes the Dex retry delay, capped at one hour. A
`Failure` carries only the status and DeepSeek's `error.type` and
`error.code` tokens, never the message, prompt, or text. The Receipt's
provider request ID is the `x-ds-trace-id` response header, which DeepSeek
sends but does not document.

While DeepSeek queues a request, it keeps the connection open with SSE
`: keep-alive` comments and closes it if inference has not started after 10
minutes, as its [rate limit page](https://api-docs.deepseek.com/quick_start/rate_limit)
documents. The keep-alives count as progress. A stream that sends no byte,
keep-alives included, for 5 minutes returns Retry instead of waiting for the
request timeout.

Streamed text reaches the Step's text Stream as it arrives. After a Retry the
Stream can hold the interrupted attempt's text followed by the whole text of
the next attempt; the Result's `Text` is the only authoritative text.

## Step defaults

| Option | Default |
| --- | --- |
| Execute durability | `sync` |
| Execute timeout | 1200 seconds; each HTTP exchange is bounded at 1170 seconds |
| Heartbeat timeout | 60 seconds; the pipeline heartbeats every 5 seconds while a call is in flight |
| Retry | 2-second initial interval, backoff 2, 60-second maximum interval, 4 attempts, 30 minutes total |

The Execute timeout leaves room for DeepSeek's 10-minute queue before a
generation starts. A generation usually exceeds the seven-second ASYNC local
phase, and an async fallback attempt would call DeepSeek again, so Execute
uses `sync`. An exchange still streaming after 1170 seconds returns Retry and
bills the tokens again, so bound `MaxOutputTokens` for long `max`-effort
tasks. Override short calls through `StepOptionsOverride`.

Tools, image and file input, the `user_id` field, Chat Prefix and FIM
Completion, and DeepSeek's Anthropic and Responses endpoints are not part of
this release.

## Example

The [summarize-text example](examples/summarize-text/README.md) runs one Flow
from Dex Web **Start Flow**: it persists the request, asks DeepSeek for a
summary with thinking off while streaming it, and completes with a generated,
truncated, or blocked outcome.

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
structured generation, with thinking off, on the connection's default model.
It reads a dedicated key from the environment and never prints it:

```bash
DEEPSEEK_CONNECTOR_TEST_API_KEY=... GOWORK=off go test -tags=live -run TestLive ./...
```

Set `DEEPSEEK_CONNECTOR_TEST_MODEL` to test another model. An exhausted
balance returns HTTP 402, which the connector classifies as
`providerRejected` with `QUOTA_EXHAUSTED`.
