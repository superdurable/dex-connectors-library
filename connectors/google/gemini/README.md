# Gemini Connector

The Gemini Connector is an independent Go module for the Google Gemini API
[`models.generateContent`](https://ai.google.dev/api/generate-content) method.
It provides `gemini.NewGenerateContentStep`, the native `generateContent`
Query, for Gemini's own request fields such as `Contents`, `ThinkingBudget`,
and an unchanged `responseJsonSchema`.

Provider-neutral text generation Steps use the
[llm connector](../../superdurable/llm/README.md) with `provider: gemini`. Its
`generateText` runs the same Gemini models, validates structured output
against the application's schema, and lets an application switch providers
by changing only the connection.

Install a published component release:

```bash
go get github.com/superdurable/dex-connectors-library/connectors/google/gemini@v0.21.0
```

## Connection

The connection kind is `gemini-api-key`. Its one secret field, `api_key`, is a
Gemini API key from Google AI Studio. The connector sends it only in the
`x-goog-api-key` header, never in the URL, disables HTTP redirects so the
header is never forwarded, and keeps it out of Results, Failures, Receipts,
Streams, and formatted values.

An application declares the connection in `dex-app.yaml`. Dex Web
**Connections** renders the manifest form for this API-key connection, and
the person who creates the connection enters the key and may set the
connection's default model in the form's `model` text field. Each Flow Step
can then pick its own model from Gemini's live model list; see
[Choose a model per Step](#choose-a-model-per-step). Dex Web saves the
settings and the key in the project configuration, and the application opens
the typed Connection once at startup, as
[`examples/generate-summary/main.go`](examples/generate-summary/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
return runWorker(ctx, logger, project)
```

```go
connection, err := gemini.NewProjectConnection(project, generatesummary.ConnectionName)
if err != nil {
	return err
}
```

[`sdkgo/projectconfig`](../../../sdkgo/projectconfig/README.md#application-loading)
documents the `DEX_PROJECT_*` environment that `LoadFromEnvironment` reads.
Credentials are reread before every provider call, so a replaced key takes
effect without a restart. Configuration is captured at startup:

| Field | Default | Meaning |
| --- | --- | --- |
| `model` | `gemini-3.5-flash-lite` | Model that `generateContent` calls when the request sets no `Model`. Blank uses the default. Accepts `gemini-3.8-flash` or `models/gemini-3.8-flash`; `New` and `NewProjectConnection` reject anything else. |
| `endpoint` | `https://generativelanguage.googleapis.com/v1beta` | API base URL. HTTPS is required except for loopback test servers. |
| `maxResponseBytes` | `8388608` (8 MiB) | Larger responses select `invalidResponse`; nothing is truncated silently. |

`gemini-3.5-flash-lite` and `gemini-3.8-flash` are the stable models that
Google [recommends for new projects](https://ai.google.dev/gemini-api/docs/models);
Google limits the Gemini 2.5 models to projects that used them before. See
[pricing](https://ai.google.dev/gemini-api/docs/pricing) before changing the
default.

`gemini.WithHTTPClient` supplies a custom transport. The connector uses a copy
of that client, disables its redirects, and applies a 270-second timeout when
the client sets none.

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

Every Step config sets `ConnectionName` to the name of its Connection, which
the generated factory checks when the Flow registers.

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
`Text`, or use the llm connector's `generateText`, which does both.

`GenerateContentResponse.Text` concatenates the non-thought text parts of the
first candidate. `Usage` reports prompt, candidate, thoughts, cached-content,
and total tokens. `ResponseID`, `ModelVersion`, `FinishReason`, and
`BlockReason` keep Gemini's identifiers and enum values.

## Choose a model per Step

The connector ships a Connector Studio bundle with one configuration unit,
`modelPicker` (`gemini.UIUnitModelPicker`, output port
`gemini.UIModelPickerPortModel`). A Flow adds it to a Step's
`ConfigurationUI`, as the example Step does, and Dex Web **Connections** then
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
The generate-summary example reads it like this:

```go
loaded, err := provider.LoadOperationConfiguration[generatesummary.SummaryModelConfiguration](
	configuration, generatesummary.SummaryModelConfigurationRef(),
)
if errors.Is(err, projectconfig.ErrObjectNotFound) {
	return generatesummary.SummaryModelConfiguration{}, nil
}
```

## Branches, retry, and Query semantics

`generateContent` is a Query with no idempotency key. Google documents it as
stateless: it creates no provider resource, and the Gemini API accepts no
idempotency key. Repeating it after an ambiguous failure is safe and only
bills the tokens again, so a lost response retries instead of selecting an
`uncertain` branch. This differs from `openai.createResponse`, which creates a
stored Response and is therefore a Mutation.

| Branch | Required | Selected when |
| --- | --- | --- |
| `generated` | yes | The first candidate finished with `STOP` and has text. |
| `truncated` | no | The candidate stopped at `MAX_TOKENS`; `Text` holds any partial output. |
| `blocked` | no | Any `promptFeedback.blockReason`, or the candidate stopped for `SAFETY`, `RECITATION`, `LANGUAGE`, `BLOCKLIST`, `PROHIBITED_CONTENT`, `SPII`, an image-policy reason, `ESCALATION`, or `PUP_LIMITED_DISABLED`. `Text` is empty. |
| `providerRejected` | no | A conclusive HTTP rejection, such as 400, 402, 403, 404, 501, or a redirect. |
| `invalidResponse` | no | The response is malformed, larger than `maxResponseBytes`, has no candidate, or finished for another reason such as `OTHER`. |
| `defect` | no | Local input, credentials, or connection configuration is invalid. |

An unwired optional branch fails the Flow. HTTP 408, 429, and 5xx other than
501, plus transport failures, return Retry. A `google.rpc.RetryInfo`
`retryDelay` or a `Retry-After` header becomes the Dex retry delay, capped at
one hour. A `Failure` carries only the HTTP status, Google status name, and
`ErrorInfo` reason, such as `provider returned HTTP 429 RESOURCE_EXHAUSTED`. It
never contains the provider body, headers, prompt, or candidate text. Gemini
reports a daily free-tier quota as the same retryable 429, so a Step retries
it until its retry policy ends.

## Step defaults

| Option | `generateContent` |
| --- | --- |
| Execute durability | `sync` |
| Execute timeout | 300 seconds; each exchange is bounded at 270 seconds |
| Heartbeat timeout | 300 seconds |
| Retry | 2-second initial interval, backoff 2, 60-second maximum interval, 5 attempts, 10 minutes total |

A long-form generation usually exceeds the seven-second ASYNC local phase, and
an async fallback attempt would call Gemini again, so Execute uses `sync`.

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
`ExecuteMethodTimeout`, replace these defaults.

Streaming (`streamGenerateContent`), tools, multimodal parts, safety settings,
cached content, multiple candidates, and `generateContent`'s `thinkingLevel`
are not part of this release.

## Migrate from generateText

This release removes the provider-neutral `generateText` Query,
`gemini.NewGenerateTextStep`, and the `GenerateTextRequest` and
`GenerateTextResponse` aliases. Move each generation Step to the
[llm connector](../../superdurable/llm/README.md): save an `llm` connection
with `provider: gemini`, the same API key, and the same `model`, and replace
`gemini.NewGenerateTextStep` with `llm.NewGenerateTextStep`; the request and
response types are the same `textgen` types. `generateContent`, its branches,
and its defaults are unchanged.

## Example

The [generate-summary example](examples/generate-summary/README.md) runs one
Flow that asks Gemini for a JSON Schema summary through `generateContent` and
completes with the decoded summary or a truncated or blocked outcome.

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
structured generation with `gemini-3.5-flash-lite`, the connection's default
model. It reads a dedicated key from the environment and never prints it:

```bash
GEMINI_CONNECTOR_TEST_API_KEY=... GOWORK=off go test -tags=live -run TestLive ./...
```
