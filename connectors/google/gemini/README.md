# Gemini Connector

The Gemini Connector is an independent Go module for the Google Gemini API
[`models.generateContent`](https://ai.google.dev/api/generate-content) method.
It provides two Queries over that method:

- `gemini.NewGenerateTextStep`, the provider-neutral `generateText` Query that
  every lab connector shares, for text or JSON Schema structured output,
  thinking levels, token usage, and explicit safety outcomes. The contract is
  specified in
  [Text generation connectors](../../../docs/connector-contract.md#text-generation-connectors).
- `gemini.NewGenerateContentStep`, the native `generateContent` Query, for
  Gemini's own request fields such as `ThinkingBudget` and an unchanged
  `responseJsonSchema`.

Install a published component release:

```bash
go get github.com/superdurable/dex-connectors-library/connectors/google/gemini@v0.3.0
```

## Connection

The connection kind is `gemini-api-key`. Its one secret field, `api_key`, is a
Gemini API key from Google AI Studio. The connector sends it only in the
`x-goog-api-key` header, never in the URL, disables HTTP redirects so the
header is never forwarded, and keeps it out of Results, Failures, Receipts,
Streams, and formatted values.

Dex Web **Connections** renders the manifest form for this API-key connection.
The person who creates the connection enters the key and may set the
connection's default model in the form's `model` text field. Each Flow Step
can then pick its own model from Gemini's live model list; see
[Choose a model per Step](#choose-a-model-per-step). Applications load the
local development store and create the typed Connection once at startup, as
[`examples/summarize-text/main.go`](examples/summarize-text/main.go) does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := gemini.NewLocalConnection(store, summarizetext.ConnectionName)
if err != nil {
	return err
}
```

Credentials are reread before every provider call, so a replaced key takes
effect without a restart. Configuration is captured at startup and applies to
both operations:

| Field | Default | Meaning |
| --- | --- | --- |
| `model` | `gemini-3.5-flash-lite` | Model that every request on this connection calls unless the request sets `Model`. Blank uses the default. Accepts `gemini-3.8-flash` or `models/gemini-3.8-flash`; `New` and `NewLocalConnection` reject anything else. |
| `endpoint` | `https://generativelanguage.googleapis.com/v1beta` | API base URL. HTTPS is required except for loopback test servers. |
| `maxResponseBytes` | `8388608` (8 MiB) | Larger responses select `invalidResponse`; nothing is truncated silently. |

`gemini-3.5-flash-lite` and `gemini-3.8-flash` are the stable models that
Google [recommends for new projects](https://ai.google.dev/gemini-api/docs/models);
Google limits the Gemini 2.5 models to projects that used them before. See
[pricing](https://ai.google.dev/gemini-api/docs/pricing) before changing the
default.

`gemini.WithHTTPClient` supplies a custom transport. The connector uses
copies of that client, disables their redirects, and applies a 270-second
timeout for `generateContent` and an 870-second timeout for `generateText`
when the client sets none. `generateText` also bounds a longer client timeout
at 870 seconds, so an exchange returns Retry before its 900-second Execute
timeout.

## Generate text

The [summarize-text Flow](examples/summarize-text/flow/workflow.go) wires the
Step and writes the summary to its text Stream:

```go
dex.DefineStep(gemini.NewGenerateTextStep(gemini.GenerateTextStepConfig[SummaryRequest]{
	StepType: summarizeTextStepType, ConnectionName: ConnectionName,
	Annotations: sdkgo.StepAnnotations{
		GroupID: "summary", GroupLabel: "Summary",
		Explanation: "Ask a Gemini model for a short summary of the submitted text.",
	},
	ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
		ID: "summaryModel", UnitID: gemini.UIUnitModelPicker, Label: "Summary model",
		Description: "Choose the Gemini model that writes the summary, or keep the connection's model.",
		Bindings:    []sdkgo.ConnectorUIBinding{{Port: gemini.UIModelPickerPortModel, JSONPointer: "/model"}},
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
func (flow *Flow) MapToGenerateTextRequest(request SummaryRequest) gemini.GenerateTextRequest {
	return gemini.GenerateTextRequest{
		Model:           flow.summaryModel.Model,
		Instructions:    "Summarize the user's text in at most three sentences. Use only facts stated in the text.",
		Messages:        []llm.Message{{Role: llm.MessageRoleUser, Text: request.Text}},
		MaxOutputTokens: maxSummaryOutputTokens,
	}
}
```

`gemini.GenerateTextRequest` and `gemini.GenerateTextResponse` alias
`llm.TextGenerationRequest` and `llm.TextGenerationResponse`, so the same
application code runs against another lab by changing the connection. The
request maps to the documented REST body:

| `GenerateTextRequest` | Gemini API |
| --- | --- |
| `Model` (`gemini-3.8-flash` or `models/gemini-3.8-flash`; empty uses the connection `model`) | path `models/{model}:generateContent` |
| `Instructions` | `systemInstruction.parts[0].text` |
| `Messages[]` (`user`, or `assistant` sent as `model`) | `contents[]` |
| `StructuredOutput` | `generationConfig.responseJsonSchema` with `responseMimeType: application/json`; `Description` becomes the schema's root description |
| `MaxOutputTokens` (0 omits) | `generationConfig.maxOutputTokens`, which includes thought tokens |
| `Temperature` (0 to 2; nil omits) | `generationConfig.temperature` |
| `ReasoningEffort` (`minimal`, `low`, `medium`, or `high`) | `generationConfig.thinkingConfig.thinkingLevel` |

Gemini 3 models always think, so `ReasoningEffortNone`, `xhigh`, and `max`
select `defect` without a request. So does `minimal` on `gemini-3.7-flash`,
`gemini-3.8-flash`, and `gemini-3.1-pro`, which Google documents with `low`,
`medium`, and `high` only, and any effort on a Gemini 1 or 2 model, which
rejects `thinkingLevel`. An alias such as `gemini-flash-latest` follows the
newest release, and the newest Flash and Pro lack `minimal`, so `minimal` on
`gemini-flash-latest` or `gemini-pro-latest` selects `defect` too. Another
model accepts all four levels, so a model Google adds needs no release. Google recommends leaving `Temperature` and
`ReasoningEffort` unset for Gemini 3 models, as the example does. The
thought text stays private; `Usage.ReasoningTokens` reports
`thoughtsTokenCount`, and `Usage.OutputTokens` adds it to
`candidatesTokenCount`.

Structured output must stay inside the portable schema subset. Google's
[`GenerationConfig` reference](https://ai.google.dev/api/generate-content#GenerationConfig)
lists `minimum`, `maximum`, `minItems`, `maxItems`, and `format` as supported,
so `generateText` sends the first four to Gemini unchanged. The reference
omits `minLength` and `maxLength` and the `uuid` format, so `generateText`
moves `minLength`, `maxLength`, and every `format` into the node's
description. The connector validates the returned text against the
application's original schema and selects `invalidResponse` on a mismatch,
so every bound is still enforced. A live `gemini-3.5-flash-lite` call
rejected one schema that carried all six bounds together with HTTP 400
`INVALID_ARGUMENT`; that call does not show which keyword Gemini rejected, so
the opt-in live test sends the four documented bounds on their own.

The call does not stream, so the text Stream receives the whole text once,
after Gemini answers. The Result's `Text` is the only authoritative text.

## Generate content

The [generate-summary Flow](examples/generate-summary/flow/workflow.go) wires
the native Step and maps its start input to a JSON Schema request:

```go
dex.DefineStep(gemini.NewGenerateContentStep(gemini.GenerateContentStepConfig[SummaryRequest]{
	StepType: generateSummaryStepType, ConnectionName: ConnectionName,
	Annotations: sdkgo.StepAnnotations{
		GroupID: "gemini", GroupLabel: "Gemini",
		Explanation: "Ask Gemini for a structured JSON summary of the submitted text.",
	},
	ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
		ID: "summaryModel", UnitID: gemini.UIUnitModelPicker, Label: "Summary model",
		Description: "Choose the Gemini model that writes the summary, or keep the connection's model.",
		Bindings:    []sdkgo.ConnectorUIBinding{{Port: gemini.UIModelPickerPortModel, JSONPointer: "/model"}},
	}}},
	Connection:          flow.connection,
	MapToOperationInput: flow.MapToGenerateContentRequest,
	Generated:           sdkgo.GoTo(summaryGenerated{}),
	Truncated:           sdkgo.GoTo(summaryNotGenerated{}),
	Blocked:             sdkgo.GoTo(summaryNotGenerated{}),
})),
```

```go
func (flow *Flow) MapToGenerateContentRequest(request SummaryRequest) gemini.GenerateContentRequest {
	return gemini.GenerateContentRequest{
		Model: flow.summaryModel.Model,
		SystemInstruction: "You write concise newsletter summaries. Use only facts stated in the provided text. " +
			"Return a headline, a two-sentence summary, and up to five key points.",
		Contents: []gemini.Content{{Role: "user", Parts: []gemini.Part{{
			Text: "Title: " + request.Title + "\n\n" + request.Text,
		}}}},
		ResponseJSONSchema: SummarySchema(),
		MaxOutputTokens:    maxSummaryOutputTokens,
	}
}
```

The request maps to the documented REST body:

| `GenerateContentRequest` | Gemini API |
| --- | --- |
| `Model` (`gemini-3.5-flash-lite` or `models/gemini-3.5-flash-lite`; empty uses the connection `model`) | path `models/{model}:generateContent` |
| `SystemInstruction` | `systemInstruction.parts[0].text` |
| `Contents[].Role` (`user`, `model`, or empty) and `Parts[].Text` | `contents[]` |
| `ResponseMIMEType` | `generationConfig.responseMimeType` |
| `ResponseJSONSchema` | `generationConfig.responseJsonSchema`; defaults the MIME type to `application/json` |
| `Temperature` (0 to 2, nil omits) | `generationConfig.temperature` |
| `MaxOutputTokens` (0 omits) | `generationConfig.maxOutputTokens` |
| `ThinkingBudget` (nil omits, -1 dynamic, 0 off where the model allows it, positive budget) | `generationConfig.thinkingConfig.thinkingBudget` |

For Gemini 3 models, leave `Temperature` nil, because Google recommends their
default of 1.0, and leave `ThinkingBudget` nil, because they treat
`thinkingBudget` as a legacy setting that may not turn thinking off.
`MaxOutputTokens` includes thought tokens.

The connector sends `responseJsonSchema`, not the deprecated OpenAPI-subset
`responseSchema`. Invalid local input, such as an invalid model ID, an empty part,
an unknown role, or an out-of-range temperature, selects `defect` before any
provider call.

`generateContent` sends `ResponseJSONSchema` unchanged, and Gemini accepts only
part of JSON Schema. In a live `gemini-3.5-flash-lite` run, a response schema
with `maxLength`, `minLength`, `maxItems`, `minItems`, `minimum`, and `maximum`
bounds selected `providerRejected` with HTTP 400 `INVALID_ARGUMENT`; the same
schema without those bounds generated. When a schema is rejected, remove the
bounds from the request and enforce them in the application after decoding
`Text`, or use `generateText`, which does both.

`GenerateContentResponse.Text` concatenates the non-thought text parts of the
first candidate. `Usage` reports prompt, candidate, thoughts, cached-content,
and total tokens. `ResponseID`, `ModelVersion`, `FinishReason`, and
`BlockReason` keep Gemini's identifiers and enum values.

## Choose a model per Step

The connector ships a Connector Studio bundle with one configuration unit,
`modelPicker` (`gemini.UIUnitModelPicker`, output port
`gemini.UIModelPickerPortModel`). A Flow adds it to a Step's
`ConfigurationUI`, as both example Steps do, and Dex Web **Connections** then
shows a tab for that Step. The tab lists models live, so a model Google adds
appears without a connector release, through two commands that Dex Web runs
with the stored key; the key never reaches the browser frame:

1. `listModels`: `GET https://generativelanguage.googleapis.com/v1beta/models`
   with `pageSize=1000`, following `nextPageToken`, and the key in the
   `x-goog-api-key` header. Models whose `supportedGenerationMethods` include
   `generateContent` are shown with their display name, token limits, and a
   `thinking` badge; the rest stay behind **Show all models**.
2. `listOpenAICompatibleModels`:
   `GET https://generativelanguage.googleapis.com/v1beta/openai/models` with
   the key as a bearer token. Dex Web through Dex CLI v0.13.8 rejects the
   header credential before sending, so the bundle falls back to this list,
   which has no capability field.

Either way, IDs that name embedding, Imagen, Veo, TTS, image, audio, or live
models stay behind **Show all models**. The user can also keep the
connection's model, which saves an empty `model`, or type any model ID, for
example when neither list answers.

The application reads the pick once at startup and passes it as the request
`Model`. An empty pick falls back to the connection's `model`, and that falls
back to `gemini-3.5-flash-lite`. Restart the application after saving a pick.
The summarize-text example reads it like this:

```go
loaded, err := localconfig.LoadOperationConfiguration[summarizetext.SummaryModelConfiguration](
	store, summarizetext.SummaryModelConfigurationRef(),
)
if errors.Is(err, localconfig.ErrConfigurationNotFound) {
	return summarizetext.SummaryModelConfiguration{}, nil
}
```

Upgrading an existing `v0.1.0`, `v0.2.0`, or `v0.2.1` connection: Dex Web
through Dex CLI v0.13.8 shows a connection saved for another connector version
as **Conflict**. Change that record's `moduleVersion` to `v0.3.0` in the
connection file that Dex Web shows, or remove the record and save the connection
again. `v0.3.0` adds `generateText` and keeps `generateContent`, its branches,
and its defaults unchanged.

## Branches, retry, and Query semantics

Both operations are Queries with no idempotency key. Google documents
`generateContent` as stateless: it creates no provider resource, and the
Gemini API accepts no idempotency key. Repeating it after an ambiguous
failure is safe and only bills the tokens again, so a lost response retries
instead of selecting an `uncertain` branch. This differs from
`openai.createResponse`, which creates a stored Response and is therefore a
Mutation.

| Branch | Required | `generateText` selects it when | `generateContent` selects it when |
| --- | --- | --- | --- |
| `generated` | yes | The first candidate finished with `STOP` and has text. | The same. |
| `truncated` | no | The candidate stopped at `MAX_TOKENS`; `Text` holds any partial output. | The same. |
| `blocked` | no | `promptFeedback.blockReason` is set, reported as `providerFinishReason` `blockReason:SAFETY` and so on, or `blockReason:UNDOCUMENTED` for a well-formed reason Google adds later, or the candidate stopped for `SAFETY`, `RECITATION`, `LANGUAGE`, `BLOCKLIST`, `PROHIBITED_CONTENT`, `SPII`, an image-policy reason, `ESCALATION`, or `PUP_LIMITED_DISABLED`. `Text` is empty. | Any `promptFeedback.blockReason`, or the same candidate reasons. |
| `providerRejected` | no | A conclusive rejection: 400 `INVALID_ARGUMENT` (authentication when the reason is `API_KEY_INVALID`), 401, 402 depleted prepay credits with `QUOTA_EXHAUSTED`, 403 `PERMISSION_DENIED`, 404 unknown model, or 501. | A conclusive HTTP rejection, such as 400, 402, 403, 404, 501, or a redirect. |
| `invalidResponse` | no | The response is malformed, larger than `maxResponseBytes`, a redirect, or has no candidate, an ill-formed block reason, or another finish reason such as `OTHER`, or structured output does not match its schema. The served model, response ID, and usage are kept when Gemini reported them. | The response is malformed, larger than `maxResponseBytes`, has no candidate, or finished for another reason such as `OTHER`. |
| `defect` | no | Local input, credentials, or connection configuration is invalid, including an effort or temperature the model does not accept. | Local input, credentials, or connection configuration is invalid. |

An unwired optional branch fails the Flow. HTTP 408, 429, and 5xx other than
501, plus transport failures, return Retry. A `google.rpc.RetryInfo`
`retryDelay` or a `Retry-After` header becomes the Dex retry delay, capped at
one hour. A `Failure` carries only the HTTP status, Google status name, and
`ErrorInfo` reason, such as `provider returned HTTP 429 RESOURCE_EXHAUSTED`. It
never contains the provider body, headers, prompt, or candidate text. Gemini
reports a daily free-tier quota as the same retryable 429, so a Step retries
it until its retry policy ends.

## Step defaults

| Option | `generateText` | `generateContent` |
| --- | --- | --- |
| Execute durability | `sync` | `sync` |
| Execute timeout | 900 seconds; each exchange is bounded at 870 seconds | 300 seconds; each exchange is bounded at 270 seconds |
| Heartbeat timeout | 60 seconds; the pipeline heartbeats every 5 seconds while a call is in flight | 300 seconds |
| Retry | 2-second initial interval, backoff 2, 60-second maximum interval, 4 attempts, 30 minutes total | 2-second initial interval, backoff 2, 60-second maximum interval, 5 attempts, 10 minutes total |

A long-form generation usually exceeds the seven-second ASYNC local phase, and
an async fallback attempt would call Gemini again, so Execute uses `sync`.

`generateText` runs on the shared pipeline, which records a Dex heartbeat
every 5 seconds while Gemini is silent, so a lost Worker is detected within
the 60-second heartbeat timeout. The call does not stream, so nothing signals
a stalled Gemini before the 870-second exchange bound.

`generateContent` sends no heartbeat until Gemini answers. The real Dex
integration test shows that a silent 15-second generation is abandoned and
retried under a 10-second heartbeat timeout, so its heartbeat timeout equals
its Execute timeout. A Worker crash is still detected immediately: the same
test kills a Worker in the middle of a generation, and the replacement Worker
retries it within seconds. The test fails if that recovery takes 60 seconds,
so a crash that waited for the heartbeat timeout is caught.

Override short `generateContent` calls, such as request interpretation with
thinking disabled, through `StepOptionsOverride`. Its non-zero Execute fields,
such as `ExecuteDurability: dex.StepDurabilityAsync` and a 30-second
`ExecuteMethodTimeout`, replace these defaults. Keep `generateText` on `sync`
with an Execute timeout above its 870-second exchange bound: an async fallback
attempt sends the call to Gemini again and bills it twice, and a shorter
timeout ends the attempt before a stalled exchange can return Retry.

Streaming (`streamGenerateContent`), tools, multimodal parts, safety settings,
cached content, multiple candidates, and `generateContent`'s `thinkingLevel`
are not part of this release.

## Examples

- The [summarize-text example](examples/summarize-text/README.md) runs one
  Flow from Dex Web **Start Flow**: it persists the request, asks Gemini for a
  summary through `generateText`, writes it to a text Stream, and completes
  with a generated, truncated, or blocked outcome.
- The [generate-summary example](examples/generate-summary/README.md) runs one
  Flow that asks Gemini for a JSON Schema summary through `generateContent`
  and completes with the decoded summary or a truncated or blocked outcome.

## Verify

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

The real Dex suites, including the shared `llmtest` scenarios for
`generateText`, need a running `dexcli dev`:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1
```

Opt-in live tests make, for each operation, one unbilled unknown-model request
and one tiny structured generation with `gemini-3.5-flash-lite`, the
connection's default model. `generateText` makes one more tiny generation whose
schema carries the `minimum`, `maximum`, `minItems`, and `maxItems` bounds it
sends to Gemini. They read a dedicated key from the environment and never
print it:

```bash
GEMINI_CONNECTOR_TEST_API_KEY=... GOWORK=off go test -tags=live -run TestLive ./...
```

Set `GEMINI_CONNECTOR_TEST_MODEL` to test another model. A project without
prepay credits returns HTTP 402, which the connector classifies as
`providerRejected`.
