# Mistral AI Connector

The Mistral Connector is an independent Go module for the
[Mistral AI](https://docs.mistral.ai/)
[Chat Completions](https://docs.mistral.ai/studio/conversations/chat-completion)
endpoint. It provides `mistral.NewGenerateTextStep`, the provider-neutral
`generateText` Query that every lab connector shares, for Mistral Large,
Medium, Small, and Ministral text or JSON Schema structured output, reasoning
effort, token usage, and streamed text. The contract is specified in
[Text generation connectors](../../docs/connector-contract.md#text-generation-connectors).

Install a published component release:

```bash
go get github.com/superdurable/dex-connectors-library/connectors/mistral@v0.1.0
```

## Connection

The connection kind is `mistral-api-key`. Its one secret field, `api_key`, is
a Mistral API key from Mistral AI Studio. The connector sends it only as an
`Authorization: Bearer` header to the connection's Mistral endpoint, never
follows a redirect, and keeps it out of Results, Failures, Receipts, Streams,
and formatted values.

Dex Web **Connections** renders the manifest form for this connection.
Applications load the local development store and create the typed
Connection once at startup, as
[`examples/mistral-summarize-text/main.go`](examples/mistral-summarize-text/main.go) does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := mistral.NewLocalConnection(store, summarizetext.ConnectionName)
if err != nil {
	return err
}
```

Credentials are reread before every provider call, so a replaced key takes
effect without a restart. Configuration is captured at startup:

| Field | Default | Meaning |
| --- | --- | --- |
| `model` | `mistral-large-2512` | Model for every request on this connection unless the request sets `Model`. Blank uses the default. |
| `endpoint` | `https://api.mistral.ai/v1` | One of the three documented base URLs; `/chat/completions` is appended. Blank uses the global endpoint. |
| `maxResponseBytes` | `8388608` (8 MiB) | Larger responses select `invalidResponse`; nothing is truncated silently. |

`mistral-large-2512` is [Mistral Large 3](https://docs.mistral.ai/models/mistral-large-3-25-12),
a generally available multimodal model with a 256k context window, priced
between Mistral Small 4 and Mistral Medium 3.5. It is the pinned
`major-minor` identifier, so its behavior never changes silently; the
`mistral-large-latest` alias moves to newer models
([model lifecycle](https://docs.mistral.ai/inference/model-lifecycle)).
A retired model returns HTTP 404, which selects `providerRejected`.

Mistral documents [three endpoints](https://docs.mistral.ai/inference/regional-inference),
and one key works on all of them:

| `endpoint` | Constant | Inference location |
| --- | --- | --- |
| `https://api.mistral.ai/v1` | `mistral.GlobalEndpoint` | Not region-specific |
| `https://api.eu.mistral.ai/v1` | `mistral.EUEndpoint` | EU and EFTA data centers, at 1.1× list price |
| `https://api.us.mistral.ai/v1` | `mistral.USEndpoint` | US data centers, at 1.1× list price |

`New` rejects any other endpoint, so the key never reaches another host. A
regional endpoint serves only the models hosted in its region; a model it
does not serve selects `providerRejected`.

`mistral.WithHTTPClient` supplies a custom transport, such as a proxy. The
connector uses a copy of that client that never follows redirects.
`mistral.WithBaseURLForTest` points the connection at a loopback fake
provider, such as `llmtest.FakeProvider`; `New` rejects any other host.

## Generate text

The [summarize-text Flow](examples/mistral-summarize-text/flow/workflow.go) wires the
Step and streams the summary to its text Stream:

```go
dex.DefineStep(mistral.NewGenerateTextStep(mistral.GenerateTextStepConfig[SummaryRequest]{
	StepType: summarizeTextStepType, ConnectionName: ConnectionName,
	Annotations: sdkgo.StepAnnotations{
		GroupID: "summary", GroupLabel: "Summary",
		Explanation: "Ask a Mistral model for a short summary of the submitted text.",
	},
	ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
		ID: "summaryModel", UnitID: mistral.UIUnitModelPicker, Label: "Summary model",
		Description: "Choose the Mistral model that writes the summary, or keep the connection's model.",
		Bindings:    []sdkgo.ConnectorUIBinding{{Port: mistral.UIModelPickerPortModel, JSONPointer: "/model"}},
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
func (flow *Flow) MapToGenerateTextRequest(request SummaryRequest) mistral.GenerateTextRequest {
	return mistral.GenerateTextRequest{
		Model:           flow.summaryModel.Model,
		Instructions:    "Summarize the user's text in at most three sentences. Use only facts stated in the text.",
		Messages:        []llm.Message{{Role: llm.MessageRoleUser, Text: request.Text}},
		MaxOutputTokens: maxSummaryOutputTokens,
	}
}
```

`mistral.GenerateTextRequest` and `mistral.GenerateTextResponse` alias
`llm.TextGenerationRequest` and `llm.TextGenerationResponse`, so the same
application code runs against another lab by changing the connection. The
request maps to the documented body:

| `GenerateTextRequest` | Chat Completions |
| --- | --- |
| `Model` (empty uses the connection `model`) | `model` |
| `Instructions` | the first message, with the `system` role |
| `Messages[]` (`user` or `assistant`) | `messages[]` |
| `StructuredOutput` | `response_format` `json_schema` with `strict: true` |
| `MaxOutputTokens` | `max_tokens`; the prompt plus `max_tokens` cannot exceed the model's context length |
| `Temperature` (0 to 1.5; nil omits) | `temperature` |
| `ReasoningEffort` | `reasoning_effort`: `none` or `high`, only on the adjustable-reasoning models `mistral-small-2603`, `mistral-medium-3-5`, and their aliases |

Every request sends `stream: true` and no other field: Mistral's
`ChatCompletionRequest` sets `additionalProperties: false`, so the connector
never sends `stream_options` or any field outside the table, and Mistral's
final stream chunk carries usage without it. A temperature outside 0 to 1.5,
or a reasoning effort on a model that has no
[adjustable reasoning](https://docs.mistral.ai/studio/conversations/reasoning),
selects `defect` without a request. The thinking trace of a reasoning model
stays private; Mistral reports no separate reasoning-token count.

Structured output must stay inside the portable schema subset. The connector
validates the returned text against the application's schema and selects
`invalidResponse` on a mismatch.

## Choose a model per Step

The connector ships a Connector Studio bundle with one configuration unit,
`modelPicker` (`mistral.UIUnitModelPicker`, output port
`mistral.UIModelPickerPortModel`). A Flow adds it to a Step's
`ConfigurationUI`, as the Step above does, and Dex Web **Connections** then
shows a tab for that Step. The tab lists models live through the bundle's
`listModels` command, `GET https://api.mistral.ai/v1/models`, which the Dex
Web broker runs with the stored key as a bearer token; the key never reaches
the browser frame. Because the list is live, a model Mistral adds appears
without a connector release.

Models whose card sets `capabilities.completion_chat` are shown, badged
**function calling** and **vision** from their capabilities. Deprecated
models stay visible with a **deprecated** badge, their deprecation date, and
Mistral's replacement, because they serve requests until retirement.
Embedding, OCR, moderation, and archived fine-tuned models stay behind
**Show all models**. The picker cannot read the connection's endpoint, so it
always lists the global catalog; check a pick against a regional endpoint's
own list. The user can also keep the connection's model, which saves an empty
`model`, or type any model ID when listing fails.

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
| `truncated` | no | The model stopped with `length` at `max_tokens`, or `model_length` at the context length; `Text` holds any partial output. |
| `blocked` | no | The model finished with `content_filter` or a refusal. `Text` is empty. |
| `providerRejected` | no | A conclusive rejection: 400, such as a prompt over the context window, 401, 402, 403, 404 for an unknown or retired model, or 422 validation. |
| `invalidResponse` | no | The response is malformed or larger than `maxResponseBytes`, has an unknown finish reason, including `error` and `tool_calls`, or structured output does not match its schema. |
| `defect` | no | Local input, credentials, or connection configuration is invalid. |

An unwired optional branch fails the Flow. HTTP 408, 429, and 5xx other than
501, transport failures, and a stream that disconnects before a finish reason
return Retry. Mistral's `error` finish reason is not retried: the shared
framework can map a finish reason only to a terminal outcome, so it selects
`invalidResponse`. A `Retry-After` header becomes the Dex retry delay, capped at
one hour. A `Failure` carries only the status and the `type` and `code`
tokens of Mistral's top-level error envelope, never the message, prompt, or
text. The Receipt's request ID is the `mistral-correlation-id` response
header, and it copies the `x-ratelimit-remaining` header when Mistral sends
it.

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
fallback attempt would call Mistral again, so Execute uses `sync`. Override
short calls through `StepOptionsOverride`.

Tools, multimodal input, `prompt_mode`, guardrails, service tiers, and the
Agents, Conversations, and FIM endpoints are not part of this release.

## Example

The [summarize-text example](examples/mistral-summarize-text/README.md) runs one Flow
from Dex Web **Start Flow**: it persists the request, asks a Mistral model for
a summary while streaming it, and completes with a generated, truncated, or
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
MISTRAL_CONNECTOR_TEST_API_KEY=... GOWORK=off go test -tags=live -run TestLive ./...
```

Set `MISTRAL_CONNECTOR_TEST_MODEL` to test another model and
`MISTRAL_CONNECTOR_TEST_ENDPOINT` to test a regional endpoint.
