# OpenAI Connector

The OpenAI Connector is an independent Go module for stored Responses of the
[OpenAI Responses API](https://developers.openai.com/api/reference/resources/responses/methods/create).
It provides:

- `openai.NewCreateResponseStep` for idempotent creation of a stored
  Response, with JSON Schema structured output, SSE text, progress, usage,
  and explicit uncertainty;
- `openai.NewRetrieveResponseStep` for query-first reconciliation of a stored
  Response.

Text generation Steps that need no stored Response use the
[llm connector](../superdurable/llm/README.md) with `provider: openai`. Its
provider-neutral `generateText` runs the same OpenAI models with `store:
false`, and an application switches providers by changing only the
connection.

Install a published component release:

```bash
go get github.com/superdurable/dex-connectors-library/connectors/openai@v0.21.0
```

## Connection

The connection kind is `openai-api-key`. Its one secret field, `api_key`, is
an OpenAI API key from the OpenAI platform. The connector sends it only as an
`Authorization: Bearer` header to the connection's `endpoint`, and keeps it
out of Results, Failures, Receipts, Streams, and formatted values.

An application declares the connection in `dex-app.yaml`, Dex Web
**Connections** renders the manifest form and saves the settings and the key
in the project configuration, and the application opens the typed Connection
once at startup, as
[`examples/summarize-text/main.go`](examples/summarize-text/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := openai.NewProjectConnection(project, summarizetext.ConnectionName)
if err != nil {
	return err
}
```

[`sdkgo/projectconfig`](../../sdkgo/projectconfig/README.md#application-loading)
documents the `DEX_PROJECT_*` environment that `LoadFromEnvironment` reads.
Credentials are reread before every provider call, so a replaced key takes
effect without a restart. Configuration is captured at startup:

| Field | Default | Meaning |
| --- | --- | --- |
| `model` | `gpt-6-sol` | Model that `createResponse` sends when its request names none. A request's `Model` overrides it. |
| `endpoint` | `https://api.openai.com/v1` | Base URL each operation appends its path to, such as `/responses`. HTTPS is required except for loopback test servers. |
| `maxResponseBytes` | `8388608` (8 MiB) | Largest response, counting every byte of a stream. Each streamed text delta carries about 260 bytes of event framing, so 8 MiB holds roughly 30,000 streamed output tokens. A larger `createResponse` response selects `uncertain`, and a larger `retrieveResponse` response selects `invalidResponse`. |
| `maxSseEventBytes` | `1048576` (1 MiB) | Largest server-sent event. The final Responses event repeats the whole Response, so raise it for very long outputs. |

OpenAI's [models page](https://developers.openai.com/api/docs/models)
recommends [GPT-6 Sol](https://developers.openai.com/api/docs/models/gpt-6-sol)
to balance intelligence and cost, at $2 input and $10 output per million
tokens; GPT-6 Luna costs less and GPT-6 Astra is the most capable.

`openai.WithHTTPClient` supplies a custom transport, such as a proxy; the
default client times out after two minutes. Tests point `endpoint` at a
loopback fake provider, such as `textgentest.FakeProvider`.

## Create a stored Response

The [summarize-text Flow](examples/summarize-text/flow/workflow.go) wires the
Step and streams the summary to its text Stream:

```go
dex.DefineStep(openai.NewCreateResponseStep(openai.CreateResponseStepConfig[SummaryRequest]{
	StepType: summarizeTextStepType, ConnectionName: ConnectionName,
	Annotations: sdkgo.StepAnnotations{
		GroupID: "summary", GroupLabel: "Summary",
		Explanation: "Store an OpenAI Response that summarizes the submitted text, streaming the summary while it is written.",
	},
	ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
		ID: "summaryModel", UnitID: openai.UIUnitModelPicker, Label: "Summary model",
		Description: "Choose the OpenAI model that writes the summary from OpenAI's live model list, or keep the connection's model, which an empty pick uses.",
		Bindings:    []sdkgo.ConnectorUIBinding{{Port: openai.UIModelPickerPortModel, JSONPointer: "/model"}},
	}}},
	Connection:          flow.connection,
	MapToOperationInput: flow.MapToCreateRequest,
	TextStream:          &summaryTextStream,
	Completed:           sdkgo.GoTo(recordSummaryOutcome{}),
	Failed:              sdkgo.GoTo(recordSummaryOutcome{}),
})),
```

Every Step config sets `ConnectionName` to the name of its Connection, which
the generated factory checks when the Flow registers.

```go
func (flow *Flow) MapToCreateRequest(request SummaryRequest) openai.CreateRequest {
	return openai.CreateRequest{
		Model:        flow.summaryModel.Model,
		Instructions: "Summarize the user's text in at most three sentences. Use only facts stated in the text.",
		Input:        request.Text,
	}
}
```

The request maps to the documented `POST /responses` body: `model`,
`input`, `instructions`, and, with `StructuredOutput`, `text.format`
`json_schema` with its `strict` flag. The Call ID becomes the
`Idempotency-Key` header, so a retried attempt of the same Step execution
cannot store a second Response.

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
loaded, err := provider.LoadOperationConfiguration[summarizetext.SummaryModelConfiguration](
	configuration, summarizetext.SummaryModelConfigurationRef(),
)
if errors.Is(err, projectconfig.ErrObjectNotFound) {
	return summarizetext.SummaryModelConfiguration{}, nil
}
```

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

| Operation | Branch | Selected when |
| --- | --- | --- |
| `createResponse` | `completed` (required) | OpenAI completed the Response. |
| `createResponse` | `failed` | OpenAI returned a terminal `failed` or `incomplete` Response, or a streaming `error` event. |
| `createResponse` | `providerRejected` | A conclusive 4xx refusal, such as 400, 401, 403, or 404. |
| `createResponse` | `uncertain` | The request was dispatched but its outcome cannot be confirmed: a transport failure, a 5xx, or a malformed, oversized, or interrupted response. |
| `createResponse` | `defect` | The input, the model, credentials, or connection configuration is invalid; nothing is sent. |
| `retrieveResponse` | `found` (required) | The Response exists and was returned. |
| `retrieveResponse` | `notFound` | OpenAI returned 404. |
| `retrieveResponse` | `providerRejected` | Another conclusive refusal. |
| `retrieveResponse` | `invalidResponse` | The response is malformed or larger than `maxResponseBytes`. |
| `retrieveResponse` | `defect` | The Response ID, credentials, or connection configuration is invalid. |

HTTP 429 returns Retry for both operations, honoring a `Retry-After` in
seconds; 5xx returns Retry for `retrieveResponse` and `uncertain` for
`createResponse`. Receipts hold the `x-request-id` and copy OpenAI's
`x-ratelimit-{limit,remaining,reset}-{requests,tokens}` headers.

## Step defaults

| Operation | Execute durability | Execute timeout | Retry |
| --- | --- | --- | --- |
| `createResponse` | `sync` | 150 seconds | 2-second initial interval, backoff 2, 30-second maximum interval, 5 attempts, 5 minutes total |
| `retrieveResponse` | `async` | 30 seconds | 1-second initial interval, backoff 2, 30-second maximum interval, 5 attempts, 2 minutes total |

## Migrate from generateText

This release removes the `generateText` Query, `openai.NewGenerateTextStep`, and
the `GenerateTextRequest` and `GenerateTextResponse` aliases. Move each
generation Step to the [llm connector](../superdurable/llm/README.md): save an
`llm` connection with `provider: openai`, the same API key, and the same
`model`, and replace `openai.NewGenerateTextStep` with
`llm.NewGenerateTextStep`; the request and response types are the same
`textgen` types. The `model` field now names the model `createResponse` sends
when its request names none, where an empty request model selected `defect`
before.

## Example

The [summarize-text example](examples/summarize-text/README.md) runs one Flow
from Dex Web **Start Flow**: it persists the request, stores an OpenAI
Response that summarizes it while streaming the text, and completes with the
`completed` or `failed` Response.

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
structured stored Response with the connection's default model. It reads a
dedicated key from the environment and never prints it:

```bash
OPENAI_CONNECTOR_TEST_API_KEY=... GOWORK=off go test -tags=live -run TestLive ./...
```

Set `OPENAI_CONNECTOR_TEST_MODEL` to test another model.
