# OpenAI Connector

The OpenAI Connector is an independent Go module for the
[OpenAI Responses API](https://developers.openai.com/api/reference/resources/responses/methods/create).
It provides:

- `openai.NewGenerateTextStep`, the provider-neutral `generateText` Query
  that every lab connector shares, for text or JSON Schema structured output,
  reasoning effort, token usage, and streamed text without storing the
  Response;
- `openai.NewCreateResponseStep` for idempotent creation of a stored
  Response, with SSE text, progress, usage, and explicit uncertainty;
- `openai.NewRetrieveResponseStep` for query-first reconciliation of a stored
  Response.

The `generateText` contract is specified in
[Text generation connectors](../../docs/connector-contract.md#text-generation-connectors).

Install a published component release:

```bash
go get github.com/superdurable/dex-connectors-library/connectors/openai@v0.7.0
```

## Connection

The connection kind is `openai-api-key`. Its one secret field, `api_key`, is
an OpenAI API key from the OpenAI platform. The connector sends it only as an
`Authorization: Bearer` header to the connection's `endpoint`, and keeps it
out of Results, Failures, Receipts, Streams, and formatted values.
`generateText` uses a copy of the HTTP client that never follows redirects.

Dex Web **Connections** renders the manifest form for this connection.
Applications load the local development store and create the typed
Connection once at startup, as
[`examples/summarize-text/main.go`](examples/summarize-text/main.go) does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := openai.NewLocalConnection(store, summarizetext.ConnectionName)
if err != nil {
	return err
}
```

Credentials are reread before every provider call, so a replaced key takes
effect without a restart. Configuration is captured at startup:

| Field | Default | Meaning |
| --- | --- | --- |
| `model` | `gpt-6-sol` | Model for every `generateText` request on this connection unless the request sets `Model`. Blank uses the default. `createResponse` always sends its request's model. |
| `endpoint` | `https://api.openai.com/v1` | Base URL each operation appends its path to, such as `/responses`. HTTPS is required except for loopback test servers. An endpoint with user information, a query, or a fragment still works for `createResponse` and `retrieveResponse`, as in `v0.6.0`, but `generateText` selects `defect` for it without a request. |
| `maxResponseBytes` | `8388608` (8 MiB) | Largest response, counting every byte of a stream. Each streamed text delta carries about 260 bytes of event framing, so 8 MiB holds roughly 30,000 streamed `generateText` output tokens; raise it for longer outputs. A larger `generateText` response selects `invalidResponse` after OpenAI has billed it; nothing is truncated silently. |
| `maxSseEventBytes` | `1048576` (1 MiB) | Largest server-sent event. The final Responses event repeats the whole Response, so raise it for very long outputs. |

OpenAI's [models page](https://developers.openai.com/api/docs/models)
recommends [GPT-6 Sol](https://developers.openai.com/api/docs/models/gpt-6-sol)
to balance intelligence and cost, at $2 input and $10 output per million
tokens; GPT-6 Luna costs less and GPT-6 Astra is the most capable.

`openai.WithHTTPClient` supplies a custom transport, such as a proxy.
`generateText` bounds each exchange at 870 seconds when that client sets no
timeout or a longer one; the other operations keep the client's timeout. Tests
point `endpoint` at a loopback fake provider, such as `llmtest.FakeProvider`.

## Generate text

The [summarize-text Flow](examples/summarize-text/flow/workflow.go) wires the
Step and streams the summary to its text Stream:

```go
dex.DefineStep(openai.NewGenerateTextStep(openai.GenerateTextStepConfig[SummaryRequest]{
	StepType: summarizeTextStepType, ConnectionName: ConnectionName,
	Annotations: sdkgo.StepAnnotations{
		GroupID: "summary", GroupLabel: "Summary",
		Explanation: "Ask an OpenAI model for a short summary of the submitted text.",
	},
	ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
		ID: "summaryModel", UnitID: openai.UIUnitModelPicker, Label: "Summary model",
		Description: "Choose the OpenAI model that writes the summary, or keep the connection's model.",
		Bindings:    []sdkgo.ConnectorUIBinding{{Port: openai.UIModelPickerPortModel, JSONPointer: "/model"}},
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
func (flow *Flow) MapToGenerateTextRequest(request SummaryRequest) openai.GenerateTextRequest {
	return openai.GenerateTextRequest{
		Model:           flow.summaryModel.Model,
		Instructions:    "Summarize the user's text in at most three sentences. Use only facts stated in the text.",
		Messages:        []llm.Message{{Role: llm.MessageRoleUser, Text: request.Text}},
		MaxOutputTokens: maxSummaryOutputTokens,
	}
}
```

`openai.GenerateTextRequest` and `openai.GenerateTextResponse` alias
`llm.TextGenerationRequest` and `llm.TextGenerationResponse`, so the same
application code runs against another lab by changing the connection. The
request maps to the documented `POST /responses` body:

| `GenerateTextRequest` | Responses API |
| --- | --- |
| `Model` (empty uses the connection `model`) | `model` |
| `Instructions` | `instructions` |
| `Messages[]` (`user` or `assistant`) | `input[]` messages with string `content` |
| `StructuredOutput` | `text.format` `json_schema` with `strict: true` |
| `MaxOutputTokens` | `max_output_tokens`, which also counts reasoning tokens |
| `Temperature` (0 to 2; nil omits) | `temperature` |
| `ReasoningEffort` | `reasoning.effort`, with the same value, such as `xhigh` for `ReasoningEffortExtraHigh` |

Every request sends `store: false`, because the Responses API stores a
Response by default and `generateText` is a Query; use `createResponse` for
a stored Response. Requests send `stream: true`, except for `o1-pro`,
`o3-pro`, and `gpt-5.5-pro`, whose model pages do not list streaming.

The connector applies the limits each model page documents, and selects
`defect` without a request for a request outside them:

| Models | `ReasoningEffort` | `Temperature` |
| --- | --- | --- |
| `gpt-6-astra` | `low`, `medium`, `high`, `xhigh`, `max` | not accepted |
| `gpt-6-sol`, `gpt-6-luna` | `none`, `low`, `medium`, `high`, `xhigh`, `max`; default `medium` | only with `ReasoningEffortNone` |
| `gpt-5.6-sol`, `gpt-5.6-terra`, `gpt-5.6-luna` | `none` through `max` | 0 to 2 |
| `gpt-5.5` | `none` through `xhigh` | 0 to 2 |
| `gpt-5.5-pro`, `gpt-5.4-pro`, `gpt-5.2-pro` | `medium`, `high`, `xhigh` | 0 to 2 |
| `gpt-5.4`, `gpt-5.4-mini`, `gpt-5.4-nano`, `gpt-5.2` | `none` through `xhigh`; default `none` | only when the effort is `none` or unset |
| `gpt-5.3-codex`, `gpt-5.2-codex` | `low` through `xhigh` | 0 to 2 |
| `gpt-5.1` | `none`, `low`, `medium`, `high` | 0 to 2 |
| `gpt-5-pro` | `high` | 0 to 2 |
| `gpt-5`, `gpt-5-mini`, `gpt-5-nano` | `minimal`, `low`, `medium`, `high` | 0 to 2 |
| every other model | any value | 0 to 2 |

A dated snapshot, such as `gpt-5.4-2026-03-05`, follows its family. For a
model the table does not name, including one OpenAI adds later, the request
is sent as written and an OpenAI 400 selects `providerRejected`. The
reasoning text stays private; `Usage.ReasoningTokens` reports how many
tokens it used.

Structured output must stay inside the portable schema subset, which already
satisfies OpenAI's strict subset. For a fine-tuned `ft:` model, the connector
moves `minimum`, `maximum`, `minLength`, `maxLength`, `minItems`,
`maxItems`, and `format` into the description, because fine-tuned models
reject them. The connector validates the returned text against the
application's schema and selects `invalidResponse` on a mismatch.

## Choose a model per Step

The connector ships a Connector Studio bundle with one configuration unit,
`modelPicker` (`openai.UIUnitModelPicker`, output port
`openai.UIModelPickerPortModel`). A Flow adds it to a Step's
`ConfigurationUI`, as the Step above does, and Dex Web **Connections** then
shows a tab for that Step. The tab lists models live through the bundle's
`listModels` command, `GET https://api.openai.com/v1/models`, which the Dex
Web broker runs with the stored key as a bearer token; the key never reaches
the browser frame. Because the list is live, a model OpenAI adds appears
without a connector release. The list host stays `api.openai.com` even when
the connection's `endpoint` points elsewhere.

Models are shown newest first, with any announced shutdown date. Models whose
ID names a family that the Responses API does not serve for text, such as
embedding, speech, transcription, realtime, image, moderation, search, and
legacy completion models, and models past their `shutdown_date`, stay behind
**Show all models**. A fine-tuned `ft:` model is judged by its base model, not
its suffix, so `ft:gpt-4.1-mini:acme:research-bot:abc` stays listed. The user
can also keep the connection's model, which saves an empty `model`, or type
any model ID when listing fails.

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

Upgrading an existing `v0.6.0` connection: Dex Web through Dex CLI v0.13.8
shows a connection saved for another connector version as **Conflict**. Change
that record's `moduleVersion` to `v0.7.0` in the connection file that Dex Web
shows, or remove the record and save the connection again.

## Branches, retry, and Query semantics

`generateText` is a Query with no idempotency key: with `store: false` it
creates no provider resource, and OpenAI documents no generation idempotency
key, so a repeated call only bills the tokens again. This differs from
`createResponse`, which stores a Response and is therefore a Mutation.

| Branch | Required | Selected when |
| --- | --- | --- |
| `generated` | yes | The Response finished with status `completed` and has text. |
| `truncated` | no | The Response is `incomplete` for `max_output_tokens`; `Text` holds any partial output. |
| `blocked` | no | The Response is `incomplete` for `content_filter` or holds a refusal, or an HTTP error, stream `error` event, or failed Response reports `misalignment_policy_violation`, `bio_policy`, or the [cybersecurity check](https://developers.openai.com/api/docs/guides/safety-checks/cybersecurity)'s `cyber_policy`. `Text` is empty. |
| `providerRejected` | no | A conclusive rejection: 400, 401, 403, 404, 501, or a 429 whose error type or code is `insufficient_quota`, `credit_balance_exhausted`, `organization_spend_limit_exceeded`, `project_spend_limit_exceeded`, or `organization_usage_limit_exceeded` (`QUOTA_EXHAUSTED`). |
| `invalidResponse` | no | The response is malformed, larger than `maxResponseBytes`, has another status or incomplete reason, carries an unrecognized error, or structured output does not match its schema. |
| `defect` | no | Local input, credentials, or connection configuration is invalid. |

An unwired optional branch fails the Flow. HTTP 408, other 429s, and 5xx
other than 501, transport failures, and a stream that ends before its
terminal event return Retry. So does a failed Response or stream `error`
event whose code is `server_error` or `rate_limit_exceeded`. A `Retry-After`
header becomes the Dex retry delay, capped at one hour. A `Failure` carries
only the status and OpenAI's error type and code, never the message, prompt,
or text. The Receipt holds the `x-request-id` and copies OpenAI's
`x-ratelimit-{limit,remaining,reset}-{requests,tokens}` headers.

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
fallback attempt would call OpenAI again, so Execute uses `sync`. Override
short calls through `StepOptionsOverride`.

Tools, image and file input, background mode, reasoning summaries,
`previous_response_id` conversation state, and `text.verbosity` are not part
of `generateText` in this release.

## Stored Responses

Create one `openai.Connection` from a configured client and logical
`sdkgo.ConnectionRef`, then pass it to the operation-specific factory.
The generated connection type prevents cross-connector wiring and rejects
serialization.

Applications own and register the Result Attribute and any structured/text
Dex Streams. Stream data is best-effort; the committed Result and branch are
authoritative.

`CreateResponse` keeps `failed` for an actual failed or incomplete OpenAI
response. Other conclusive API refusals use `providerRejected`.
`RetrieveResponse` additionally exposes `notFound` and `invalidResponse`.
Invalid local input or connection configuration uses the standard `defect`
branch, while an ambiguous dispatched mutation uses `uncertain`.

When a factory supplies progress or text Streams, `CreateResponse` sends
`stream=true`. Its bounded SSE reader accepts lifecycle, text-delta, completed,
failed, incomplete, and error events while ignoring unknown event types. The
completed Response is authoritative for value and usage. Early EOF or a
missing terminal event is uncertain and can be reconciled with
`RetrieveResponse`.

## Example

The [summarize-text example](examples/summarize-text/README.md) runs one Flow
from Dex Web **Start Flow**: it persists the request, asks an OpenAI model
for a summary while streaming it, and completes with a generated, truncated,
or blocked outcome.

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
OPENAI_CONNECTOR_TEST_API_KEY=... GOWORK=off go test -tags=live -run TestLive ./...
```

Set `OPENAI_CONNECTOR_TEST_MODEL` to test another model. A project without
credits or past a spend limit returns a 429 billing code, which the connector
classifies as `providerRejected` with `QUOTA_EXHAUSTED`.
