# LLM Connector

The LLM Connector is an independent Go module that runs one provider-neutral
`generateText` Query against OpenAI, Claude, or Gemini, choosing the provider
from a `provider/model` string such as `anthropic/claude-sonnet-5`. It
provides `llmrouter.NewGenerateTextStep` and a Studio model picker that
merges the live model lists of the providers a connection adds. One
connection holds any of the three providers, each with its own key. The
Query follows
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
go get github.com/superdurable/dex-connectors-library/connectors/superdurable/llm@v0.2.0
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

Every provider method has the connection kind `llm-api-keys`. Dex Web
**Connectors** renders the manifest form, and applications load the local
development store and create
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

One connection holds one or more providers. The manifest declares each
provider as an auth method with `selection: multiple`, so the Connectors
form shows a **Providers** section: **Add provider** adds a card for OpenAI,
Claude, or Gemini, and each card holds that provider's key, its setup guide,
and its own fields. A new form starts with the OpenAI card; remove it when you
do not use OpenAI. Save needs at least one provider.

| Provider | Method ID | Key field | Where to get the key | Card fields |
| --- | --- | --- | --- | --- |
| OpenAI | `openai` | `openai_api_key`, starting `sk-` | [Platform > API keys](https://platform.openai.com/api-keys) | none |
| Claude | `anthropic` | `anthropic_api_key`, starting `sk-ant-` | [Claude Console > Settings > API keys](https://platform.claude.com/settings/keys) | `anthropicWorkspaceId` |
| Gemini | `gemini` | `gemini_api_key`, starting `AIza` | [Google AI Studio > API keys](https://aistudio.google.com/api-keys) | none |

A provider's key is required while its card is added. Claude's card also
takes `anthropicWorkspaceId`, a `wrkspc_` workspace ID from Claude Console >
Settings > Workspaces that the Claude connector sends as
`anthropic-workspace-id`. A multi-workspace Anthropic key requires it; leave
it blank for a key scoped to one workspace. Any other value fails `New`.

The saved credentials list the added providers in add order as
`auth_methods`, which `Credentials.AuthMethodIDs` holds, and only the added
providers' keys. A Step may pick any model of an added provider. A model
whose provider is not added selects `defect` with `AUTHENTICATION` and no
request, even when the credentials still hold that provider's key.

Each key is sent only by its own provider connector, only in that provider's
credential header, and only to that provider's host: OpenAI and Claude
receive `Authorization: Bearer`, and Gemini receives `x-goog-api-key`. The
provider connectors never follow a redirect, and no key enters a Result,
Failure, Receipt, Stream, log, or formatted value. Credentials, including the
list of added providers, are reread before every provider call, so a replaced
key or an added or removed provider takes effect without a restart;
configuration is captured at startup.

After the providers come the connection fields:

| Field | Default | Meaning |
| --- | --- | --- |
| `model` | blank | The default model for Steps that pick none: `provider/model`, or a provider alone for that provider connector's default model. Its provider must be added. Blank uses the first added provider's default model. An invalid value fails `New`, so the Worker does not start. |
| `maxResponseBytes` | `8388608` (8 MiB) | The response limit for every provider, counting every byte of a stream. Larger responses select `invalidResponse`. OpenAI adds about 260 bytes of event framing per streamed text delta, so the default holds roughly 30,000 `openai/` output tokens; raise it for longer outputs. |

The `model` field declares `studioUnit: {unit: modelPicker, port: model}`.
Once the connection is saved, Dex Web renders this connector's model picker
for it, listing the added providers' live models, and saves the pick into the
connection's `model`. Before the first save the form says to save the
connection first. A host without Studio units in the connection form shows a
plain text input.

The other provider settings keep each provider connector's defaults: Claude's
`defaultMaxOutputTokens` of 16000, OpenAI's 1 MiB `maxSseEventBytes`, and the
public endpoints. OpenAI's final stream event repeats the whole response, so
an `openai/` response larger than 1 MiB selects `invalidResponse` whatever
`maxResponseBytes` allows; use the OpenAI connector for such outputs.

### Editing a saved connection

On Dex Web releases with the provider form, the form shows each stored key as
stored; leave it blank to keep it, or enter a new key to replace it. Removing
a provider's card drops its key and its card fields, such as the Claude
workspace ID. The default model and the other connection fields are shown
with their saved values.

The provider form, kept keys, and the connection's model picker need Dex CLI
`cli-v1.2.0` or later. `cli-v1.1.2` and earlier ignore `selection: multiple`,
show one radio choice per provider, and cannot save that form.

### Migrate from v0.1.0

v0.1.0 held three optional keys, a required `model`, and
`anthropicWorkspaceId` in one flat form. A v0.1.0 record has no
`auth_methods`, so after the upgrade every Step selects `defect` with
`AUTHENTICATION` and `connection credentials are unavailable`, without a
request, until the record is saved again. The Worker still starts, because
credentials are read per call. To migrate:

1. Clear the **Conflict** that the version change raises, as
   [below](#conflict-after-a-version-change) describes.
2. Open the connection, add each provider you use again with its key, and
   enter the Claude workspace ID in the Claude card if you had one.
3. Save, then pick the default model in the connection form, or leave it blank
   for the first added provider's default model.
4. Restart the application, which reads configuration at startup.

Code changes: `Credentials` gains `AuthMethodIDs`, which an application that
builds `Credentials` itself must fill with the providers it adds, and
`Config.Model` may be blank. Step picks and request models keep their
`provider/model` form.

### Key-format guard

Before any request, the connector rejects a key that clearly belongs to
another routed provider, so a key pasted into the wrong card is never sent to
the wrong host:

| Field | Rejected formats |
| --- | --- |
| `openai_api_key` | Claude keys (`sk-ant-`) and Google keys (`AIza`) |
| `anthropic_api_key` | every `sk-` key that does not start `sk-ant-`, which covers every OpenAI key prefix, and Google keys (`AIza`) |
| `gemini_api_key` | every `sk-` key, which covers OpenAI and Claude keys |

Any other format is sent unchanged, so the provider decides. The guard runs
after the provider check: a model whose provider is not added selects
`defect` before any key is read. The check runs on the exact value the
provider pipeline reads, so a key replaced, or a provider removed, between
the router's check and the request is checked again. It cannot protect the
Studio model lists, which Dex Web runs with the stored field.

### Conflict after a version change

A connection record stores the connector module version it was saved with.
After an upgrade of this connector, or after an application's own provider
requirement raises a provider connector version under minimal version
selection, Dex Web shows the connection as **Conflict** and hides its form
and pickers. Flows keep running, because the runtime ignores the stored
version. To recover, edit the record's `moduleVersion` in the connection file
that the Connectors page names, or delete the record and enter the values
again. The upgrade from v0.1.0 also needs the record saved again with its
providers; see [Migrate from v0.1.0](#migrate-from-v010). Provider connector
upgrades are batched into planned minor releases of this connector to keep
Conflicts rare; security fixes ship immediately.

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
`model`. When both are blank, the call uses the first added provider alone,
which is that provider connector's default model: a connection that added
Claude and then OpenAI runs `claude-sonnet-5`. An application builds the
request `Model` from the Step's model pick and any code fallback.

A selection that breaks the grammar, such as a bare `claude-sonnet-5`,
`Anthropic/claude-sonnet-5`, `anthropic/`, or an unrouted provider, selects
`defect` with `VALIDATION` and no request, and the message shows the expected
form without repeating the value. A selection whose provider is not added
selects `defect` with `AUTHENTICATION` and no request to any provider, and
the message names the provider to add, such as `the model selects anthropic,
but the connection has not added the Claude provider; add Claude to the
connection`. The first added provider is never a fallback for it. A
selection whose provider key is blank, whitespace only, or in another
provider's format also selects `defect` with `AUTHENTICATION` and no request,
even when the other keys are set. A key is never used for another provider.

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
		Description: "Choose the provider/model that writes the summary from the live lists of the providers this connection adds, or keep the connection default: the connection's model, or the first added provider's default model when it has none.",
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

The request carries the Step's pick, empty when the connection default
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
adds. Add `Model string` with the JSON name `model` to
`SummaryRequest` and change the same line to:

```go per-run-model
		Model:        cmp.Or(request.Model, flow.summaryModel.Model),
```

## Choose a model per Step

The connector ships a Connector Studio bundle with one configuration unit,
`modelPicker` (`llmrouter.UIUnitModelPicker`, output port
`llmrouter.UIModelPickerPortModel`). Dex Web **Connectors** shows a tab for
each Step that adds it, and renders the same unit for the connection's
`model` field once the connection is saved. The picker lists only the
providers the connection adds:

1. a first option that saves an empty pick. On a Step it reads **Connection
   default (anthropic/claude-sonnet-5)**, naming the connection's `model`, or
   **Connection default (the first added provider's default model)** when the
   connection has none. In the connection form it reads **Connector default
   (the first added provider's default model)**;
2. one default-model option per added provider, which saves `openai`,
   `anthropic`, or `gemini`;
3. the added providers' models, in the order OpenAI, Claude, Gemini, each
   saved as `provider/model` and badged with its provider, so searching
   `anthropic` or `gemini` finds a provider's models;
4. a `provider/model-id` entry that accepts only a selection the connector
   would accept.

A provider that is not added runs no list command and has no option. Dex Web
releases that report no added providers to the frame, which predate the
provider form, list all three providers, and their first option reads "Use
the connection's default model".

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
list adds a notice that names the key to check, the provider's default-model
option, and `provider/<model-id>` entry. Only listing is affected: generation
works for all three providers on every Dex Web release. When every list
fails, the picker adds one more notice and still offers each listed
provider's default-model option and manual entry, so a connection with only
Claude on a Dex Web release before `cli-v0.13.10` can pick **Claude default
model**.

Dex Web releases before `cli-v0.14.2` also raise a page-level "Connector
provider command failed" banner for a failed list, one banner for the page
until the next catalog load. Before `cli-v0.13.10` the Claude and native
Gemini lists always fail, so the banner appears each time the picker opens.
The banner does not affect generation. The Claude list sends no
`anthropic-workspace-id`, so a multi-workspace key cannot list Claude models;
the Claude card's `anthropicWorkspaceId` still applies to generation.

The application reads the Step pick and the connection's `model` once at
startup and passes the pick as the request `Model`; restart it after saving
either. The example reads the pick like this:

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
itself, for a model selection, a provider that is not added, or a key,
carries `llm` instead.

| Branch | Required | Selected when |
| --- | --- | --- |
| `generated` | yes | The model finished normally and returned text. |
| `truncated` | no | The model stopped at the output token limit; `Text` holds any partial output. |
| `blocked` | no | The provider stopped the response for a content policy, or the model refused. |
| `providerRejected` | no | The provider conclusively rejected the request, such as an invalid key, an unknown model, or exhausted quota. |
| `invalidResponse` | no | The provider's response was malformed, oversized, or unusable. |
| `defect` | no | The model selection, a provider the connection has not added, a missing or mismatched key, the request, or the configuration is invalid. |

Each Step attempt makes at most one provider exchange, with the same Call ID
for every attempt of the Step execution. The connector never falls back to
another provider inside an attempt. Model failover belongs in the Flow: wire
`providerRejected` or `invalidResponse` to a second llm Step with another
model, or use `dex.ProceedToOnExecuteFailure`.

A request model, a Step pick, or a connection model fixes the provider, so a
Retry returns to the same provider, and a key or provider-list change between
attempts only toggles `defect`. When all three are blank, each attempt reads
the first added provider again, so saving the connection with another first
provider between attempts moves the retry to that provider. A Worker
restarted with a new pick or connection model also maps the Step input again,
so a retrying Step can move to another provider. Its text Stream can then
hold text from both providers, and both bill. The Result's `Text` is the only
authoritative text.

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
provider connector, and prove that each key reaches only its own provider
and only while that provider is added. The real Dex suites need a running
`dexcli dev`; they load connection files that list `auth_methods`, as Dex Web
writes them:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1
```

An opt-in live test makes one tiny generation with each provider whose key
is set, using that provider's default model, and never prints a key:

```bash
LLM_CONNECTOR_TEST_OPENAI_API_KEY=... LLM_CONNECTOR_TEST_ANTHROPIC_API_KEY=... \
  LLM_CONNECTOR_TEST_GEMINI_API_KEY=... GOWORK=off go test -tags=live -run TestLive ./...
```
