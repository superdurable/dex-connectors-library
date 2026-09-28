# LLM Connector

The LLM Connector is an independent Go module that runs one provider-neutral
`generateText` Query against OpenAI, Claude, or Gemini, choosing the provider
from a `provider/model` string such as `anthropic/claude-sonnet-5`. It
provides `llmrouter.NewGenerateTextStep` and a Studio model picker that
merges the three providers' live model lists. The Query follows
[Text generation connectors](../../../docs/connector-contract.md#text-generation-connectors),
with the two [differences](#differences-from-the-text-generation-contract)
that routing requires.

Each attempt dispatches to the released `generateText` Query of the provider
connector that this module's `go.mod` pins, so a Result is exactly what a
direct call to that connector returns:

| Prefix | Provider connector | Pinned release | Public host | Key field | `Receipt.Provider` |
| --- | --- | --- | --- | --- | --- |
| `openai` | [OpenAI](../../openai/README.md) | `connectors/openai/v0.7.0` | `api.openai.com` | `openai_api_key` | `openai` |
| `anthropic` | [Claude](../../anthropic/README.md) | `connectors/anthropic/v0.1.0` | `api.anthropic.com` | `anthropic_api_key` | `claude` |
| `gemini` | [Gemini](../../google/gemini/README.md) | `connectors/google/gemini/v0.3.0` | `generativelanguage.googleapis.com` | `gemini_api_key` | `gemini` |

Install a published component release:

```bash
go get github.com/superdurable/dex-connectors-library/connectors/superdurable/llm@v0.1.0
```

The module lives in the company directory, `connectors/superdurable/llm`,
and its package is named `llmrouter`, so import it by that name:

```go
llmrouter "github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
```

Use this connector for text generation with OpenAI, Claude, or Gemini. Use a
provider's own connector for provider-native operations, such as OpenAI
`createResponse` or Gemini `generateContent`, for a provider-specific
setting such as Claude's `defaultMaxOutputTokens`, OpenAI's
`maxSseEventBytes`, or a custom endpoint, and for a lab this connector does
not route.

## Differences from the text-generation contract

This connector routes to the provider connectors instead of implementing a
wire format, so it departs from two rules of the text-generation contract:

- `GenerateText()` returns `sdkgo.Query[GenerateTextRequest, GenerateTextResponse]`,
  not `*llm.TextGenerationQuery`, because each attempt invokes the Query of
  the provider that the model selects. `llmtest.RunTextGenerationExchangeSuite`
  therefore runs on each provider connector's Query as `New` builds it, and a
  differential test proves the router returns those attempts unchanged.
- The model picker declares `listOpenAIModels`, `listAnthropicModels`,
  `listGeminiModels`, and `listGeminiOpenAICompatibleModels` under the
  `llm.models-list` capability instead of one `listModels` command on
  `api_key`, because each list is bound to its own provider's key field and
  Dex Web authorizes each command with only that field.

## Connection

The connection kind is `llm-api-keys`. Dex Web **Connections** renders the
manifest form, and applications load the local development store and create
the typed Connection once at startup, as
[`examples/summarize-text/main.go`](examples/summarize-text/main.go) does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := llmrouter.NewLocalConnection(store, summarizetext.ConnectionName)
if err != nil {
	return err
}
```

The connection holds up to three optional secret keys. Enter the key of each
provider your Steps use and leave the others blank:

- `openai_api_key` is an OpenAI project key from
  [Platform > API keys](https://platform.openai.com/api-keys), starting `sk-`.
- `anthropic_api_key` is a Claude API key from
  [Claude Console > Settings > API keys](https://platform.claude.com/settings/keys),
  starting `sk-ant-`.
- `gemini_api_key` is a Gemini API key from
  [Google AI Studio > API keys](https://aistudio.google.com/api-keys),
  starting `AIza`.

Each key is sent only by its own provider connector, only in that provider's
credential header, and only to that provider's host: OpenAI and Claude
receive `Authorization: Bearer`, and Gemini receives `x-goog-api-key`. The
provider connectors never follow a redirect, and no key enters a Result,
Failure, Receipt, Stream, log, or formatted value. Credentials are reread
before every provider call, so a replaced key takes effect without a
restart; configuration is captured at startup.

| Field | Default | Meaning |
| --- | --- | --- |
| `model` | required | The default model for Steps that pick none: `provider/model`, or a provider alone for that provider connector's default model. An invalid value fails `New`, so the Worker does not start. |
| `anthropicWorkspaceId` | blank | A `wrkspc_` workspace ID that the Claude connector sends as `anthropic-workspace-id`. A multi-workspace Anthropic key requires it. Any other value fails `New`. |
| `maxResponseBytes` | `8388608` (8 MiB) | The response limit for every provider, counting every byte of a stream. Larger responses select `invalidResponse`. OpenAI adds about 260 bytes of event framing per streamed text delta, so the default holds roughly 30,000 `openai/` output tokens; raise it for longer outputs. |

The other provider settings keep each provider connector's defaults: Claude's
`defaultMaxOutputTokens` of 16000, OpenAI's 1 MiB `maxSseEventBytes`, and the
public endpoints. OpenAI's final stream event repeats the whole response, so
an `openai/` response larger than 1 MiB selects `invalidResponse` whatever
`maxResponseBytes` allows; use the OpenAI connector for such outputs.

### Every save replaces every value

Dex Web stores the whole connection on each save and drops blank fields, so a
save that leaves a key blank removes that key. Re-enter every key, the
default model, and the workspace ID you keep each time you save. The form
reports **Authorization complete** even when no key was entered; a Step then
selects `defect` naming the missing field.

### Key-format guard

Before any request, the connector rejects a key that clearly belongs to
another routed provider, so a key pasted into the wrong field is never sent to
the wrong host:

| Field | Rejected formats |
| --- | --- |
| `openai_api_key` | Claude keys (`sk-ant-`) and Google keys (`AIza`) |
| `anthropic_api_key` | every `sk-` key that does not start `sk-ant-`, which covers every OpenAI key prefix, and Google keys (`AIza`) |
| `gemini_api_key` | every `sk-` key, which covers OpenAI and Claude keys |

Any other format is sent unchanged, so the provider decides. The check runs
on the exact value the provider pipeline reads, so a key replaced between the
router's check and the request is checked again. It cannot protect the Studio
model lists, which Dex Web runs with the stored field.

### Conflict after a version change

A connection record stores the connector module version it was saved with.
After an upgrade of this connector, or after an application's own provider
requirement raises a provider connector version under minimal version
selection, Dex Web shows the connection as **Conflict** and hides its form
and pickers. Flows keep running, because the runtime ignores the stored
version. To recover, edit the record's `moduleVersion` in the connection file
that the Connections page names, or delete the record and enter the values
again. Provider connector upgrades are batched into planned minor releases of
this connector to keep Conflicts rare; security fixes ship immediately.

## Choose a model

A model selection is `provider [ "/" model ]`:

- The provider is exactly `openai`, `anthropic`, or `gemini`, in lowercase.
- The model is everything after the first `/`. It must not be blank, and it
  is checked by that provider connector's model-ID rule: OpenAI and Claude
  accept any 1 to 256 printable ASCII characters, such as
  `openai/ft:gpt-6-sol:acme::abc123`, and Gemini accepts one path segment,
  with or without `models/`, such as `gemini/models/gemini-3.8-flash`.
- A provider alone uses that provider connector's default model:
  `gpt-6-sol`, `claude-sonnet-5`, or `gemini-3.5-flash-lite`.

The request's trimmed `Model` wins; a blank one uses the connection's
required `model`. An application builds the request `Model` from the Step's
model pick and any code fallback. A selection that breaks the grammar, such
as a bare `claude-sonnet-5`, `Anthropic/claude-sonnet-5`, `anthropic/`, or an
unrouted provider, selects `defect` with `VALIDATION` and no request, and the
message shows the expected form without repeating the value. A selection
whose provider key is blank, whitespace only, or in another provider's
format selects `defect` with `AUTHENTICATION` and no request to any
provider, even when the other keys are set. A key is never used for another
provider.

`requestedModel` in the Result is the provider's model ID without the prefix,
because the provider connector reports it. `llmrouter.QualifiedModel(result)`
restores the prefix from `Receipt.Provider`, so the value can be fed back as
a request `Model`:

```go
outcome := SummaryOutcome{
	Branch: result.Branch, Summary: result.Value.Text, Model: llmrouter.QualifiedModel(result),
	ServedModel: result.Value.ServedModel, FinishReason: result.Value.FinishReason, Usage: result.Value.Usage,
}
```

### Portable requests

Every provider connector checks the request against the chosen model and
selects `defect` without a request for a value that model does not accept. A
request that should run on any model the picker lists sets no `Temperature`,
no `ReasoningEffort`, and a zero or generous `MaxOutputTokens`:

- current Claude models accept only the default temperature of 1.0;
- `gpt-6-sol` accepts a temperature only with effort `none`, and GPT-5.x Pro
  accepts only `medium`, `high`, or `xhigh` effort;
- Claude Sonnet 4.5 and Haiku 4.5 accept no effort;
- reasoning models count thinking tokens against `MaxOutputTokens`, so a small
  limit truncates them.

## Generate text

The [summarize-text Flow](examples/summarize-text/flow/workflow.go) wires the
Step with the model picker and streams the summary to its text Stream:

```go
dex.DefineStep(llmrouter.NewGenerateTextStep(llmrouter.GenerateTextStepConfig[SummaryRequest]{
	StepType: summarizeTextStepType, ConnectionName: ConnectionName,
	Annotations: sdkgo.StepAnnotations{
		GroupID: "summary", GroupLabel: "Summary",
		Explanation: "Ask the OpenAI, Claude, or Gemini model that the Step picks for a short summary of the submitted text.",
	},
	ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
		ID: "summaryModel", UnitID: llmrouter.UIUnitModelPicker, Label: "Summary model",
		Description: "Choose the provider/model that writes the summary from the OpenAI, Claude, and Gemini lists this connection's keys reach, or keep the connection's default model.",
		Bindings:    []sdkgo.ConnectorUIBinding{{Port: llmrouter.UIModelPickerPortModel, JSONPointer: "/model"}},
	}}},
	Connection:          flow.connection,
	MapToOperationInput: flow.MapToGenerateTextRequest,
	TextStream:          &summaryTextStream,
	Generated:           sdkgo.GoTo(recordSummaryOutcome{}),
	Truncated:           sdkgo.GoTo(recordSummaryOutcome{}),
	Blocked:             sdkgo.GoTo(recordSummaryOutcome{}),
	ProviderRejected:    sdkgo.GoTo(recordSummaryFailure{}),
	InvalidResponse:     sdkgo.GoTo(recordSummaryFailure{}),
	Defect:              sdkgo.GoTo(recordSummaryFailure{}),
})),
```

The request carries the Step's pick, empty when the connection's model
applies:

```go
func (flow *Flow) MapToGenerateTextRequest(request SummaryRequest) llmrouter.GenerateTextRequest {
	return llmrouter.GenerateTextRequest{
		Model:        flow.summaryModel.Model,
		Instructions: "Summarize the user's text in at most three sentences. Use only facts stated in the text.",
		Messages:     []llm.Message{{Role: llm.MessageRoleUser, Text: request.Text}},
	}
}
```

`llmrouter.GenerateTextRequest` and `llmrouter.GenerateTextResponse` alias
`llm.TextGenerationRequest` and `llm.TextGenerationResponse`, so the same
application Step can also run on a provider connector.

### A Step that names a model

When the user names a model, such as "use Claude Opus", keep the picker and
fall back to that exact model ID in code, so Dex Web can replace a retired
model without a code change. Take the ID from the user or the provider
connector's README; never invent one. The one changed line of
`MapToGenerateTextRequest` is:

```go named-model
		Model:        cmp.Or(flow.summaryModel.Model, "anthropic/claude-opus-5-5"),
```

For a named provider only, such as "use Claude", fall back to the provider
alone, `cmp.Or(flow.summaryModel.Model, "anthropic")`.

### An opt-in per-run model

The example takes no model in its start input. Add one only when the
application must choose the model per run, because anyone who can start the
Flow can then choose any model, and bill any provider, that the connection
has a key for. Add `Model string` with the JSON name `model` to
`SummaryRequest` and change the same line to:

```go per-run-model
		Model:        cmp.Or(request.Model, flow.summaryModel.Model),
```

## Choose a model per Step

The connector ships a Connector Studio bundle with one configuration unit,
`modelPicker` (`llmrouter.UIUnitModelPicker`, output port
`llmrouter.UIModelPickerPortModel`). Dex Web **Connections** shows a tab for
each Step that adds it. The tab lists:

1. one default-model option per provider, which saves `openai`, `anthropic`,
   or `gemini`;
2. OpenAI, Claude, and Gemini models in that order, each saved as
   `provider/model` and badged with its provider, so searching `anthropic` or
   `gemini` finds a provider's models;
3. "Use the connection's default model", which saves an empty pick;
4. a `provider/model-id` entry that accepts only a selection the connector
   would accept.

Each list runs its own manifest command, and Dex Web's broker authorizes each
command with only its own key field. The key never reaches the browser
frame. The commands equal the pinned provider connectors' list commands
except for their IDs, the shared `llm.models-list` capability, and the
credential field; a test compares them with the pinned manifests.

| Command | Request | Dex Web before `cli-v0.13.10` | Dex Web `cli-v0.13.10` and later |
| --- | --- | --- | --- |
| `listOpenAIModels` | `GET https://api.openai.com/v1/models` with `openai_api_key` as a bearer token | lists | lists |
| `listAnthropicModels` | `GET https://api.anthropic.com/v1/models?limit=1000` with `anthropic_api_key` as a bearer token and `anthropic-version: 2023-06-01` | fails: the host drops the fixed header, so Claude rejects the list | lists |
| `listGeminiModels` | `GET https://generativelanguage.googleapis.com/v1beta/models?pageSize=1000` with `gemini_api_key` in `x-goog-api-key` | fails before sending: the host accepts only bearer credentials | lists, with the `generateContent` filter |
| `listGeminiOpenAICompatibleModels` | `GET https://generativelanguage.googleapis.com/v1beta/openai/models` with `gemini_api_key` as a bearer token | lists, as the fallback | not reached |

A blank key field sends nothing, and that provider's list fails. Each failed
list adds a notice that names the key to add, the provider's default-model
option, and `provider/<model-id>` entry. Only listing is affected: generation
works for all three providers on every Dex Web release. When every list
fails, the picker adds one more notice and still offers the three
default-model options and manual entry, so a connection with only an
Anthropic key on a Dex Web release before `cli-v0.13.10` can pick **Claude
default model**.

Dex Web releases before `cli-v0.14.2` also raise a page-level "Connector
provider command failed" banner for a failed list, one banner for the page
until the next catalog load. Before `cli-v0.13.10` the Claude and native
Gemini lists always fail, so the banner appears each time the picker opens.
The banner does not affect generation. The Claude list sends no
`anthropic-workspace-id`, so a multi-workspace key cannot list Claude models;
the connection's `anthropicWorkspaceId` still applies to generation.

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

## Branches, retry, and passthrough

`generateText` declares the six shared branches. Every branch, `Failure`,
`Receipt`, and Retry that a provider connector returns reaches the Step
unchanged, so each provider's README owns its status and finish-reason
table. `Receipt.Provider` and `Failure.Provider` name the serving connector:
`openai`, `claude`, or `gemini`. A `defect` that this connector selects
itself, for a model selection or a key, carries `llm` instead.

| Branch | Required | Selected when |
| --- | --- | --- |
| `generated` | yes | The model finished normally and returned text. |
| `truncated` | no | The model stopped at the output token limit; `Text` holds any partial output. |
| `blocked` | no | The provider stopped the response for a content policy, or the model refused. |
| `providerRejected` | no | The provider conclusively rejected the request, such as an invalid key, an unknown model, or exhausted quota. |
| `invalidResponse` | no | The provider's response was malformed, oversized, or unusable. |
| `defect` | no | The model selection, a missing or mismatched key, the request, or the configuration is invalid. |

Each Step attempt makes at most one provider exchange, with the same Call ID
for every attempt of the Step execution. The connector never falls back to
another provider inside an attempt. Model failover belongs in the Flow: wire
`providerRejected` or `invalidResponse` to a second llm Step with another
model, or use `dex.ProceedToOnExecuteFailure`.

Routing depends only on the request, the Step pick, and the connection model
loaded at startup, so a Retry always returns to the same provider, and a key
change between attempts only toggles `defect`. A Worker restarted with a new
pick or connection model maps the Step input again, so a retrying Step can
move to another provider. Its text Stream can then hold text from both
providers, and both bill. The Result's `Text` is the only authoritative text.

## Step defaults

| Option | Default |
| --- | --- |
| Execute durability | `sync` |
| Execute timeout | 900 seconds; each provider bounds one HTTP exchange at 870 seconds |
| Heartbeat timeout | 60 seconds; the provider pipeline heartbeats every 5 seconds while a call is in flight |
| Retry | 2-second initial interval, backoff 2, 60-second maximum interval, 4 attempts, 30 minutes total |

These equal every pinned provider connector's `generateText` defaults. Dex
applies only the llm Step's options, so `New` fails when a linked provider
connector needs a longer Execute timeout or declares other branches, which
happens when an application requires a newer provider connector than this
module's `go.mod` pins. The error names the provider connector and asks to
upgrade this connector or pin the listed version.

A hung provider returns Retry at 870 seconds, before the Execute timeout. An
`StepOptionsOverride` Execute timeout below 870 seconds lets Dex abandon an
exchange that is still running and start another, which bills twice.
`llmrouter.WithHTTPClient` supplies one transport for every provider, such
as a proxy, and `New` rejects a client `Timeout` of 900 seconds or more.
`llmrouter.WithProviderBaseURLForTest` points one provider at a fake
provider on `127.0.0.1` or `localhost`, such as `llmtest.FakeProvider`; `New`
rejects any other host.

## Example

The [summarize-text example](examples/summarize-text/README.md) runs one Flow
from Dex Web **Start Flow**: it persists the request, asks the picked model
for a summary while streaming it, and completes with a generated,
truncated, or blocked outcome, or records a rejected, invalid, or defect
outcome and fails.

## Verify

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
npm ci --prefix ../../../sdk/react && npm run build --prefix ../../../sdk/react
npm ci --prefix ui && npm test --prefix ui && npm run build --prefix ui
```

The unit suites run the shared `llmtest` exchange suite on every route as
`New` builds it, compare every routed attempt with a direct call to the
provider connector, and prove that each key reaches only its own provider.
The real Dex suites need a running `dexcli dev`:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1
```

An opt-in live test makes one tiny generation with each provider whose key
is set, using that provider's default model, and never prints a key:

```bash
LLM_CONNECTOR_TEST_OPENAI_API_KEY=... LLM_CONNECTOR_TEST_ANTHROPIC_API_KEY=... \
  LLM_CONNECTOR_TEST_GEMINI_API_KEY=... GOWORK=off go test -tags=live -run TestLive ./...
```
