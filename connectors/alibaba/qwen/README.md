# Alibaba Cloud Model Studio Qwen Connector

The Qwen Connector is an independent Go module for the
[Alibaba Cloud Model Studio](https://www.alibabacloud.com/help/en/model-studio/)
[OpenAI-compatible Chat Completions](https://www.alibabacloud.com/help/en/model-studio/qwen-api-via-openai-chat-completions)
endpoint. It provides `qwen.NewGenerateTextStep`, the provider-neutral
`generateText` Query that every lab connector shares, for Qwen text or
schema-validated JSON output, Qwen3.8 reasoning effort, token usage, and
streamed text. The contract is specified in
[Text generation connectors](../../../docs/connector-contract.md#text-generation-connectors).

Install a published component release:

```bash
go get github.com/superdurable/dex-connectors-library/connectors/alibaba/qwen@v0.1.0
```

## Connection

The connection kind is `qwen-api-key`. Its one secret field, `api_key`, is a
Model Studio API key, such as `sk-...`. Keys are
[bound to one region](https://www.alibabacloud.com/help/en/model-studio/regions):
a key from another region fails with HTTP 401. The connector sends the key
only as an `Authorization: Bearer` header to the configured `endpoint`, never
follows a redirect, and keeps it out of Results, Failures, Receipts, Streams,
and formatted values.

Dex Web **Connections** renders the manifest form for this connection.
Applications load the local development store and create the typed
Connection once at startup, as
[`examples/summarize-text/main.go`](examples/summarize-text/main.go) does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := qwen.NewLocalConnection(store, summarizetext.ConnectionName)
if err != nil {
	return err
}
```

Credentials are reread before every provider call, so a replaced key takes
effect without a restart. Configuration is captured at startup:

| Field | Default | Meaning |
| --- | --- | --- |
| `model` | `qwen3.7-plus` | Model for every request on this connection unless the request sets `Model`. Blank uses the default. |
| `endpoint` | `https://dashscope-intl.aliyuncs.com/compatible-mode/v1` | DashScope endpoint of the key's region. Blank uses Singapore. |
| `maxResponseBytes` | `8388608` (8 MiB) | Larger responses select `invalidResponse`; nothing is truncated silently. |

`qwen3.7-plus` is the model Model Studio
[recommends](https://www.alibabacloud.com/help/en/model-studio/text-generation-model)
for balanced performance and cost, between `qwen3.8-flash` and `qwen3.8-max`;
see [pricing](https://www.alibabacloud.com/help/en/model-studio/model-pricing).
It thinks before it answers by default.

`endpoint` accepts only the fixed
[DashScope endpoints](https://www.alibabacloud.com/help/en/model-studio/base-url),
with or without a trailing slash:

| Region | `endpoint` |
| --- | --- |
| Singapore (international) | `https://dashscope-intl.aliyuncs.com/compatible-mode/v1` |
| China (Hong Kong) | `https://cn-hongkong.dashscope.aliyuncs.com/compatible-mode/v1` |
| China (Beijing) | `https://dashscope.aliyuncs.com/compatible-mode/v1` |

Workspace-dedicated endpoints (`{WorkspaceId}.{region}.maas.aliyuncs.com`),
trial and plan endpoints, and the regions that offer only workspace-dedicated
endpoints (US (Virginia), Germany (Frankfurt), and Japan (Tokyo)) are out of
scope for this release, so `New` rejects them. Model Studio recommends
workspace-dedicated endpoints for production and states that the DashScope
domain gets no new features after September 30, 2026; the DashScope
endpoints remain fully functional.

`qwen.WithHTTPClient` supplies a custom transport, such as a proxy. The
connector uses a copy of that client that never follows redirects.
`qwen.WithBaseURLForTest` points the connection at a loopback fake provider,
such as `llmtest.FakeProvider`; `New` rejects any other host.

## Generate text

The [summarize-text Flow](examples/summarize-text/flow/workflow.go) wires the
Step and streams the summary to its text Stream:

```go
dex.DefineStep(qwen.NewGenerateTextStep(qwen.GenerateTextStepConfig[SummaryRequest]{
	StepType: summarizeTextStepType, ConnectionName: ConnectionName,
	Annotations: sdkgo.StepAnnotations{
		GroupID: "summary", GroupLabel: "Summary",
		Explanation: "Ask a Qwen model for a short summary of the submitted text.",
	},
	ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
		ID: "summaryModel", UnitID: qwen.UIUnitModelPicker, Label: "Summary model",
		Description: "Choose the Qwen model that writes the summary, or keep the connection's model.",
		Bindings:    []sdkgo.ConnectorUIBinding{{Port: qwen.UIModelPickerPortModel, JSONPointer: "/model"}},
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
func (flow *Flow) MapToGenerateTextRequest(request SummaryRequest) qwen.GenerateTextRequest {
	return qwen.GenerateTextRequest{
		Model:           flow.summaryModel.Model,
		Instructions:    "Summarize the user's text in at most three sentences. Use only facts stated in the text.",
		Messages:        []llm.Message{{Role: llm.MessageRoleUser, Text: request.Text}},
		MaxOutputTokens: maxSummaryOutputTokens,
	}
}
```

`qwen.GenerateTextRequest` and `qwen.GenerateTextResponse` alias
`llm.TextGenerationRequest` and `llm.TextGenerationResponse`, so the same
application code runs against another lab by changing the connection. The
request maps to the documented body:

| `GenerateTextRequest` | Chat Completions |
| --- | --- |
| `Model` (empty uses the connection `model`) | `model` |
| `Instructions` | the first message, with the `system` role, as Model Studio documents; Model Studio says not to set one for QwQ models |
| `Messages[]` (`user` or `assistant`) | `messages[]` |
| `StructuredOutput` | `response_format` `json_object`, plus a schema instruction in the system message |
| `MaxOutputTokens` | `max_completion_tokens`, which also counts thinking, on Qwen3.5 and later Plus and Flash and Qwen3.7 and later Max models; `max_tokens`, which caps only the answer, on every other model |
| `Temperature` (0 up to but excluding 2; nil omits) | `temperature` |
| `ReasoningEffort` | `reasoning_effort`, only on the Qwen3.8 models listed below |

Every request sends `stream: true` with `stream_options.include_usage`.
Model Studio recommends streaming to avoid its non-streaming timeout, and
open-source Qwen models reject non-streaming calls while thinking. The thinking
text in `reasoning_content` stays private; `Usage.ReasoningTokens` reports
how many tokens it used.

[Reasoning effort](https://www.alibabacloud.com/help/en/model-studio/deep-thinking)
is sent only where Model Studio documents it, with its native values:

| Models | Accepted `ReasoningEffort` |
| --- | --- |
| `qwen3.8-max`, `qwen3.8-max-0902`, `qwen3.8-flash`, `qwen3.8-27b` | `ReasoningEffortNone` (turns thinking off), `Low`, `Medium`, `ExtraHigh` (`xhigh`) |
| `qwen3.8-2.4t-a95b`, which always thinks | `Low`, `Medium`, `ExtraHigh` |

Any other effort or model, including `ReasoningEffortHigh`, which Qwen3.8
silently maps to `xhigh`, selects `defect` without a request, and so does a
temperature of 2 or more. Other models turn thinking on or off only through
Model Studio's `enable_thinking` field, which this release does not send, so
each model keeps its default.

[Structured output](https://www.alibabacloud.com/help/en/model-studio/qwen-structured-output)
uses JSON Object mode on every model, because Model Studio offers JSON Schema
mode only on Qwen3.7 and later models and not yet in Singapore. JSON Object
mode requires the word JSON in the messages; the schema instruction the
connector appends to the system message contains it, along with the schema.
The schema must stay inside the portable subset, and the connector validates
the returned text against it and selects `invalidResponse` on a mismatch.
Model Studio supports JSON Object mode on the Qwen3.6 and Qwen3.5 models and
the Qwen3 open-source series only in non-thinking mode, and warns that they may
return JSON that is not strictly valid while thinking. They think by default
and this release does not send `enable_thinking`, so structured output on them
can select `invalidResponse`. The Qwen3.7 and later models Model Studio lists
carry no such caveat, so prefer them for structured output.

## Choose a model per Step

The connector ships a Connector Studio bundle with one configuration unit,
`modelPicker` (`qwen.UIUnitModelPicker`, output port
`qwen.UIModelPickerPortModel`). A Flow adds it to a Step's `ConfigurationUI`,
as the Step above does, and Dex Web **Connections** then shows a tab for that
Step. The tab lists models live through Model Studio's
[List models](https://www.alibabacloud.com/help/en/model-studio/list-models)
API, `GET /api/v1/models`, which the Dex Web broker runs with the stored key
as a bearer token; the key never reaches the browser frame. Model Studio
documents `TG` (text generation) and `Reasoning` as separate capability
values, so thinking-only models such as `qwen3.8-2.4t-a95b` may not carry
`TG`, and a pinned command can send only one value. The bundle therefore
reads a `capabilities=TG` list and a `capabilities=Reasoning` list and merges
them without duplicates. Model Studio documents the list on two DashScope
hosts, so each list has one pinned command per host:

| Command | Host | Filter |
| --- | --- | --- |
| `listModels` | `https://dashscope-intl.aliyuncs.com` (Singapore) | `capabilities=TG` |
| `listModelsHongKong` | `https://cn-hongkong.dashscope.aliyuncs.com` (China (Hong Kong)) | `capabilities=TG` |
| `listReasoningModels` | `https://dashscope-intl.aliyuncs.com` (Singapore) | `capabilities=Reasoning` |
| `listReasoningModelsHongKong` | `https://cn-hongkong.dashscope.aliyuncs.com` (China (Hong Kong)) | `capabilities=Reasoning` |

A key works only in its own region, so the bundle requests the first text
generation page from Singapore and then from China (Hong Kong). The first host
that accepts the key serves every later page and the Reasoning list, and an
error on those reads is shown as is instead of falling back to the other host.
Because the lists are live, a model Model Studio adds appears without a
connector release. The bundle reads up to five pages of 100 models from each
list. Qwen models with text input and output are shown; third-party models
such as DeepSeek, Kimi, and GLM, realtime models, and models without text
output stay behind **Show all models**. A model with a scheduled offline time names it.
The user can also keep the connection's model, which saves an empty `model`,
or type any model ID. Model Studio documents the China (Beijing) list only on
workspace-dedicated hosts, so a Beijing key falls back to typing a model ID.

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
| `blocked` | no | The model finished with `content_filter` or a refusal, or content moderation returned `data_inspection_failed`, before or during the stream. `Text` is empty. |
| `providerRejected` | no | A conclusive rejection: 400, 401 `invalid_api_key`, 403 `access_denied`, 404 `model_not_found`, 501, or an account or billing error (below). |
| `invalidResponse` | no | The response is malformed, larger than `maxResponseBytes`, or has an unknown finish reason, or structured output does not match its schema. |
| `defect` | no | Local input, credentials, or connection configuration is invalid. |

These documented [error codes](https://www.alibabacloud.com/help/en/model-studio/error-code)
select `providerRejected` with `QUOTA_EXHAUSTED`, because waiting does not
fix them: 400 `Arrearage` (overdue payment), 403
`AllocationQuota.FreeTierOnly`, and 429 `CommodityNotPurchased`,
`PrepaidBillOverdue`, `PostpaidBillOverdue`, and `BudgetLimitExceeded`.

An unwired optional branch fails the Flow. HTTP 408, other 429 codes such as
`limit_requests` and `insufficient_quota` (per-minute throttling), 5xx other
than 501, transport failures, and a stream that disconnects before a finish
reason return Retry. Any other `error` object in a stream, or a `[DONE]`
before a finish reason, selects `invalidResponse`. A `Retry-After` header
becomes the Dex retry delay, capped at one hour. A `Failure` carries only the
status and Model Studio's error codes, never the message, prompt, or text.

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
fallback attempt would call Model Studio again, so Execute uses `sync`.
DashScope endpoints time out a request after 600 seconds, inside these budgets.
Override short calls through `StepOptionsOverride`.

Tools, multimodal input, web search, `enable_thinking`, `thinking_budget`,
JSON Schema mode, and the DashScope native and Responses APIs are not part of
this release.

## Example

The [summarize-text example](examples/summarize-text/README.md) runs one Flow
from Dex Web **Start Flow**: it persists the request, asks a Qwen model for a
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
QWEN_CONNECTOR_TEST_API_KEY=... GOWORK=off go test -tags=live -run TestLive ./...
```

Set `QWEN_CONNECTOR_TEST_MODEL` to test another model, and
`QWEN_CONNECTOR_TEST_ENDPOINT` for a key from China (Hong Kong) or China
(Beijing). An overdue account returns 400 `Arrearage`, which the connector
classifies as `providerRejected` with `QUOTA_EXHAUSTED`.
