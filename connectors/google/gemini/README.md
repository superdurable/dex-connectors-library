# Gemini Connector

The Gemini Connector is an independent Go module for the Google Gemini API
[`models.generateContent`](https://ai.google.dev/api/generate-content) method.
It provides `gemini.NewGenerateContentStep` for text or JSON Schema structured
output, thinking budgets, token usage, and explicit safety outcomes.

Install a published component release:

```bash
go get github.com/superdurable/dex-connectors-library/connectors/google/gemini@v0.2.1
```

## Connection

The connection kind is `gemini-api-key`. Its one secret field, `api_key`, is a
Gemini API key from Google AI Studio. The connector sends it only in the
`x-goog-api-key` header, never in the URL, disables HTTP redirects so the
header is never forwarded, and keeps it out of Results, Failures, Receipts,
and formatted values.

Dex Web **Connections** renders the manifest form for this API-key connection.
The person who creates the connection enters the key and may set the
connection's default model in the form's `model` text field. Each Flow Step
can then pick its own model from Gemini's live model list; see
[Choose a model per Step](#choose-a-model-per-step). Applications load the local development
store and create the typed Connection once at startup, as
[`examples/generate-summary/main.go`](examples/generate-summary/main.go) does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := gemini.NewLocalConnection(store, generatesummary.ConnectionName)
if err != nil {
	return err
}
```

Credentials are reread before every provider call, so a replaced key takes
effect without a restart. Configuration is captured at startup:

| Field | Default | Meaning |
| --- | --- | --- |
| `model` | `gemini-3.5-flash-lite` | Model that every request on this connection calls unless the request sets `Model`. Accepts `gemini-3.8-flash` or `models/gemini-3.8-flash`; `New` and `NewLocalConnection` reject anything else. |
| `endpoint` | `https://generativelanguage.googleapis.com/v1beta` | API base URL. HTTPS is required except for loopback test servers. |
| `maxResponseBytes` | `8388608` (8 MiB) | Larger responses select `invalidResponse`; nothing is truncated silently. |

`gemini.WithHTTPClient` supplies a custom transport. The connector uses a copy
of that client, disables its redirects, and applies a 270-second timeout when
the client sets none.

## Generate content

The [generate-summary Flow](examples/generate-summary/flow/workflow.go) wires
the Step and maps its start input to a JSON Schema request:

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

Google limits the Gemini 2.5 models to projects that used them before, so new
projects call a Gemini 3 model such as `gemini-3.5-flash-lite` or
`gemini-3.8-flash`. For Gemini 3 models, leave `Temperature` nil, because
Google recommends their default of 1.0, and leave `ThinkingBudget` nil, because
they treat `thinkingBudget` as a legacy setting that may not turn thinking off.
`MaxOutputTokens` includes thought tokens.

The connector sends `responseJsonSchema`, not the deprecated OpenAPI-subset
`responseSchema`. Invalid local input, such as an invalid model ID, an empty part,
an unknown role, or an out-of-range temperature, selects `defect` before any
provider call.

The connector sends `ResponseJSONSchema` unchanged, and Gemini accepts only
part of JSON Schema. In a live `gemini-3.5-flash-lite` run, a response schema
with `maxLength`, `minLength`, `maxItems`, `minItems`, `minimum`, and `maximum`
bounds selected `providerRejected` with HTTP 400 `INVALID_ARGUMENT`; the same
schema without those bounds generated. When a schema is rejected, remove the
bounds from the request and enforce them in the application after decoding
`Text`.

`GenerateContentResponse.Text` concatenates the non-thought text parts of the
first candidate. `Usage` reports prompt, candidate, thoughts, cached-content,
and total tokens. `ResponseID`, `ModelVersion`, `FinishReason`, and
`BlockReason` keep Gemini's identifiers and enum values.

## Choose a model per Step

The connector ships a Connector Studio bundle with one configuration unit,
`modelPicker` (`gemini.UIUnitModelPicker`, output port
`gemini.UIModelPickerPortModel`). A Flow adds it to a Step's
`ConfigurationUI`, as the Step above does, and Dex Web **Connections** then
shows a tab for that Step. The tab lists models live through the bundle's
`listModels` command, which Dex Web runs with the stored key:
`GET https://generativelanguage.googleapis.com/v1beta/openai/models`. That
OpenAI-compatible list accepts the key as a bearer token, which the Dex Web
broker injects; the key never reaches the browser frame. Because the list is
live, a model Google adds appears without a connector release.

The list has no capability field, so IDs that name embedding, Imagen, Veo,
TTS, audio, or live models stay behind **Show all models**. The user can also
keep the connection's model, which saves an empty `model`, or type any model
ID, for example when the key cannot list models.

The application reads the pick once at startup and passes it as the request
`Model`. An empty pick falls back to the connection's `model`, and that falls
back to `gemini-3.5-flash-lite`. Restart the application after saving a pick.
The example reads it like this:

```go
loaded, err := localconfig.LoadOperationConfiguration[generatesummary.SummaryModelConfiguration](
	store, generatesummary.SummaryModelConfigurationRef(),
)
```

Upgrading an existing `v0.1.0` or `v0.2.0` connection: Dex Web through Dex CLI
v0.13.8 shows a connection saved for another connector version as
**Conflict**. Change that record's `moduleVersion` to `v0.2.1` in the
connection file that Dex Web shows, or remove the record and save the connection
again.

## Branches, retry, and Query semantics

`generateContent` is a Query with no idempotency key. Google documents the
method as stateless: it creates no provider resource, and the Gemini API
accepts no idempotency key. Repeating it after an ambiguous failure is safe
and only bills the tokens again, so a lost response retries instead of
selecting an `uncertain` branch. This differs from `openai.createResponse`,
which creates a stored Response and is therefore a Mutation.

| Branch | Required | Selected when |
| --- | --- | --- |
| `generated` | yes | The first candidate finished with `STOP` and has text. |
| `truncated` | no | The candidate stopped at `MAX_TOKENS`; `Text` holds any partial output. |
| `blocked` | no | `promptFeedback.blockReason` is set, or the candidate stopped for `SAFETY`, `RECITATION`, `LANGUAGE`, `BLOCKLIST`, `PROHIBITED_CONTENT`, `SPII`, an image-policy reason, `ESCALATION`, or `PUP_LIMITED_DISABLED`. `Text` is empty. |
| `providerRejected` | no | A conclusive HTTP rejection, such as 400 `INVALID_ARGUMENT`, 402 depleted prepay credits, 403 `PERMISSION_DENIED`, 404 unknown model, or 501. |
| `invalidResponse` | no | The response is malformed, larger than `maxResponseBytes`, has no candidate, or finished for another reason such as `OTHER`. |
| `defect` | no | Local input, credentials, or connection configuration is invalid. |

An unwired optional branch fails the Flow. HTTP 408, 429, and 5xx other than
501, plus transport failures, return Retry. A `google.rpc.RetryInfo`
`retryDelay` or a `Retry-After` header becomes the Dex retry delay, capped at
one hour. A `Failure` carries only the HTTP status and Google status name,
such as `provider returned HTTP 429 RESOURCE_EXHAUSTED`. It never contains the
provider body, headers, prompt, or candidate text.

## Step defaults

| Option | Default |
| --- | --- |
| Execute durability | `sync` |
| Execute timeout | 300 seconds |
| Heartbeat timeout | 300 seconds |
| Retry | 2-second initial interval, backoff 2, 60-second maximum interval, 5 attempts, 10 minutes total |

A long-form generation usually exceeds the seven-second ASYNC local phase, so
Execute uses `sync`. The call does not stream, so a healthy attempt sends no
heartbeat until Gemini answers. The real Dex integration test shows that a
silent 15-second generation is abandoned and retried under a 10-second
heartbeat timeout, so the heartbeat timeout equals the Execute timeout. A
Worker crash is still detected immediately: the same test kills a Worker in
the middle of a generation, and the replacement Worker retries it within
seconds. The test fails if that recovery takes 60 seconds, so a crash that
waited for the heartbeat timeout is caught.

Override short calls, such as request interpretation with thinking disabled,
through `StepOptionsOverride`. Its non-zero Execute fields, such as
`ExecuteDurability: dex.StepDurabilityAsync` and a 30-second
`ExecuteMethodTimeout`, replace these defaults.

Streaming (`streamGenerateContent`), tools, multimodal parts, safety settings,
cached content, multiple candidates, and Gemini 3 `thinkingLevel` are not part
of this release.

## Example

The [generate-summary example](examples/generate-summary/README.md) runs one
Flow from Dex Web **Start Flow**: it persists the request, asks Gemini for a
JSON Schema summary, and completes with the decoded summary or a truncated or
blocked outcome.

## Verify

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

The real Dex suite needs a running `dexcli dev`:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1
```

An opt-in live test makes one unbilled unknown-model request and one tiny
structured generation with `gemini-3.5-flash-lite`, the model the example
calls. It reads a dedicated key from the environment and never prints it:

```bash
GEMINI_CONNECTOR_TEST_API_KEY=... GOWORK=off go test -tags=live -run TestLive ./...
```

Set `GEMINI_CONNECTOR_TEST_MODEL` to test another model. A project without
prepay credits returns HTTP 402, which the connector classifies as
`providerRejected`.
