# LLM Connector

The LLM Connector is the one text generation connector for Dex
applications. Its provider-neutral `generateText` Query runs a model of the
provider that the connection names, with that provider's API key and its own
model IDs, and returns the six shared branches. It provides
`llm.NewGenerateTextStep` and a Studio model picker that lists the
connection's provider's live models. The Query follows
[Text generation connectors](../../../docs/connector-contract.md#text-generation-connectors).

| Provider | `provider` | API and wire format | Default model | Regions | Key |
| --- | --- | --- | --- | --- | --- |
| OpenAI | `openai` | Responses API, `POST https://api.openai.com/v1/responses` | `gpt-6-sol` | `global` | `sk-` project key |
| Claude | `anthropic` | Messages API, `POST https://api.anthropic.com/v1/messages` | `claude-sonnet-5` | `global` | `sk-ant-` key |
| Gemini | `gemini` | `POST https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent` | `gemini-3.5-flash-lite` | `global` | `AIza` key |
| Qwen | `qwen` | Model Studio Chat Completions, `https://dashscope-intl.aliyuncs.com/compatible-mode/v1` | `qwen3.7-plus` | `global` (Singapore), `hong-kong`, `china` (Beijing) | `sk-` key of the same region |
| DeepSeek | `deepseek` | Chat Completions, `https://api.deepseek.com` | `deepseek-flash` | `global` | `sk-` key |
| Meta | `meta` | Meta Model API Chat Completions, `https://api.meta.ai/v1` | `muse-spark-1.3` | `global` | `LLM\|<id>\|<secret>` team key |
| Mistral | `mistral` | Chat Completions, `https://api.mistral.ai/v1` | `mistral-large-2512` | `global`, `eu`, `us` | workspace key |
| Kimi | `kimi` | Kimi API Chat Completions, `https://api.moonshot.ai/v1` | `kimi-k2.6` | `global` (platform.kimi.ai), `china` (platform.kimi.com) | `sk-` key of the same platform |
| xAI | `xai` | Chat Completions, `https://api.x.ai/v1` | `grok-4.3` | `global`, `us` | `xai-` team key |

The module depends only on the Connector Go SDK, so an application never
links a second provider connector for text generation, and a provider
change is a connection change, not a code change. Use this connector for
every text generation Step. Provider-native operations stay in their own
connectors: OpenAI's stored `createResponse` and `retrieveResponse`, and
Gemini's `generateContent`.

Install a published component release:

```bash
go get github.com/superdurable/dex-connectors-library/connectors/superdurable/llm@v0.21.0
```

The package name, `llm`, is the last element of the module path, so import
it without an alias:

```go
"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
```

## Connection

An application declares a named `llm` connection in `dex-app.yaml`. Dex Web
**Connections** renders the manifest form, saves the settings and the key in
the project configuration, and the application opens the typed Connection
once at startup, as [`examples/summarize-text/main.go`](examples/summarize-text/main.go)
does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := llm.NewProjectConnection(project, summarizetext.ConnectionName)
if err != nil {
	return err
}
```

[`sdkgo/projectconfig`](../../../sdkgo/projectconfig/README.md#application-loading)
documents the `DEX_PROJECT_*` environment that `LoadFromEnvironment` reads.
`NewProjectConnection` reads the settings once and resolves the key from
project storage during every call, so a replaced key takes effect without a
restart; a settings change needs one. Settings decode strictly, so a field the
manifest does not declare stops the Worker at startup.

One connection names one provider and holds one key:

| Field | Default | Meaning |
| --- | --- | --- |
| `provider` | required | `openai`, `anthropic`, `gemini`, `qwen`, `deepseek`, `meta`, `mistral`, `kimi`, or `xai`. Every Step on the connection calls this provider. Use one connection per provider. |
| `api_key` | required, secret | The provider's API key from its key page below. It is sent only to that provider's API host: in `x-goog-api-key` for Gemini and as an `Authorization` bearer token for every other provider. |
| `model` | blank | The model for Steps that pick none, written as the provider names it, such as `claude-opus-5-5`. Blank uses the provider's default model in the table above. |
| `region` | `global` | The regional API platform of the key; see [Regions](#regions). |
| `anthropicWorkspaceId` | blank | Only for `anthropic` with a key that spans several workspaces: the `wrkspc_` ID from Claude Console > Settings > Workspaces, sent as `anthropic-workspace-id`. Any other provider with a value stops the Worker. |
| `maxResponseBytes` | blank | The complete response limit in bytes, counting every byte of a stream. Blank uses 64 MiB for `deepseek`, whose streams carry keep-alive comments and reasoning chunks, and 8 MiB for every other provider. Larger responses select `invalidResponse`. |

The `model` field declares `studioUnit: {unit: modelPicker, port: model}`, so
once the connection is saved, Dex Web renders the model picker for it. The
authorization guide links every provider's key page:

| Provider | Key page |
| --- | --- |
| OpenAI | [Platform > API keys](https://platform.openai.com/api-keys) |
| Claude | [Claude Console > Settings > API keys](https://platform.claude.com/settings/keys) |
| Gemini | [Google AI Studio > API keys](https://aistudio.google.com/api-keys) |
| Qwen | [Model Studio](https://bailian.console.aliyun.com/) > API Key, in the key's region |
| DeepSeek | [DeepSeek Platform > API keys](https://platform.deepseek.com/api_keys) |
| Meta | [Meta Model API dashboard](https://dev.meta.ai/) > API keys |
| Mistral | [Mistral AI Studio > API Keys](https://console.mistral.ai/api-keys) |
| Kimi | [Kimi Platform](https://platform.kimi.ai/) > Console > API Keys, or platform.kimi.com for a China key |
| xAI | [xAI Console](https://console.x.ai/) > API Keys |

`New` validates the connection before the Worker starts: a missing or
unknown provider, a region the provider does not serve, a model ID the
provider's model-ID rule rejects, or an `anthropicWorkspaceId` on another
provider is an error that never repeats the configured model or workspace
ID. `New` makes no provider request.

### Regions

Every provider serves `global`. Four providers serve another region, each a
fixed host, so a configured region never sends the key elsewhere:

| `provider` | `region` | API base URL |
| --- | --- | --- |
| `qwen` | `global` | `https://dashscope-intl.aliyuncs.com/compatible-mode/v1` (Singapore) |
| `qwen` | `hong-kong` | `https://cn-hongkong.dashscope.aliyuncs.com/compatible-mode/v1` |
| `qwen` | `china` | `https://dashscope.aliyuncs.com/compatible-mode/v1` (Beijing) |
| `mistral` | `eu` | `https://api.eu.mistral.ai/v1`, inference in the EU and EFTA at 1.1 times list price |
| `mistral` | `us` | `https://api.us.mistral.ai/v1`, inference in the United States at 1.1 times list price |
| `kimi` | `china` | `https://api.moonshot.cn/v1`, for keys issued on platform.kimi.com |
| `xai` | `us` | `https://us.api.x.ai/v1`, which currently serves only `grok-4.7` and `grok-4.6` |

Qwen and Kimi keys work only on the platform that issued them; a key used in
the wrong region selects `providerRejected` with `AUTHENTICATION`. A
regional Mistral or xAI endpoint serves only the models hosted there.

## Choose a model

A model is the provider's own model ID, such as `gpt-6-luna`,
`claude-haiku-4-5`, or `qwen3.8-flash`. It is never prefixed with the
provider: the connection's `provider` selects the API. The request's trimmed
`Model` wins; a blank one uses the connection's `model`, and a blank
connection model uses the provider's default model. An application builds
the request `Model` from the Step's model pick and any code fallback.

Every provider except Gemini accepts any model ID of 1 to 256 printable
ASCII characters without spaces, such as the OpenAI fine-tune
`ft:gpt-6-sol:acme::abc123`. Gemini puts the model in the URL path, so it
accepts one path segment of letters, digits, `.`, `_`, and `-`, with or
without `models/`. Any other model selects `defect` with `VALIDATION` and no
request, and the message never repeats the value. A model the provider does
not serve, including one written as `provider/model`, reaches the provider,
which rejects it with `providerRejected`.

`requestedModel` in the Result is the model the request was sent for and
`Receipt.Provider` is the connection's provider, so the pair identifies the
model exactly.

### Portable requests

Each provider checks the request against the chosen model and selects
`defect` without a request for a field that model does not accept. A request
that should run on any model the picker lists sets no `Temperature`, no
`ReasoningEffort`, and a zero or generous `MaxOutputTokens`:

- Claude models after Opus 4.6 accept only the default temperature of 1.0,
  and Kimi and DeepSeek accept no temperature;
- reasoning efforts differ per provider and model, as the
  [provider reference](#provider-reference) lists;
- reasoning models count thinking tokens against `MaxOutputTokens`, so a small
  limit truncates them.

## Generate text

The [summarize-text Flow](examples/summarize-text/flow/workflow.go) wires the
Step with the model picker and streams the summary to its text Stream:

```go
dex.DefineStep(llm.NewGenerateTextStep(llm.GenerateTextStepConfig[SummaryRequest]{
	StepType: summarizeTextStepType, ConnectionName: ConnectionName,
	Annotations: sdkgo.StepAnnotations{
		GroupID: "summary", GroupLabel: "Summary",
		Explanation: "Ask the model that the Step picks, from the connection's provider, for a short summary of the submitted text.",
	},
	ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
		ID: "summaryModel", UnitID: llm.UIUnitModelPicker, Label: "Summary model",
		Description: "Choose the model that writes the summary from the live model list of the connection's provider, or type one of its model IDs. Keep the connection default to use the connection's model, or the provider's default model when the connection has none.",
		Bindings:    []sdkgo.ConnectorUIBinding{{Port: llm.UIModelPickerPortModel, JSONPointer: "/model"}},
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

Every Step config sets `ConnectionName` to the name of its Connection, which
the generated factory checks when the Flow registers. The request carries the
Step's pick, empty when the connection default applies:

```go
func (flow *Flow) MapToGenerateTextRequest(request SummaryRequest) llm.GenerateTextRequest {
	return llm.GenerateTextRequest{
		Model:        flow.summaryModel.Model,
		Instructions: "Summarize the user's text in at most three sentences. Use only facts stated in the text.",
		Messages:     []textgen.Message{{Role: textgen.MessageRoleUser, Text: request.Text}},
	}
}
```

`llm.GenerateTextRequest` and `llm.GenerateTextResponse` alias
`textgen.TextGenerationRequest` and `textgen.TextGenerationResponse` from
`github.com/superdurable/dex-connectors-library/sdkgo/textgen`, which also
defines `Message`, `StructuredOutput`, `ReasoningEffort`, `FinishReason`, and
`Usage`.

### A Step that names a model

When the user names a model, such as "use Claude Opus", keep the picker and
fall back to that exact model ID in code, so Dex Web can replace a retired
model without a code change. The model must belong to the connection's
provider; take the ID from the user or the provider's documentation, never
invent one. The one changed line of `MapToGenerateTextRequest` is:

```go named-model
		Model:        cmp.Or(flow.summaryModel.Model, "claude-opus-5-5"),
```

When the user names only a provider, such as "use Claude", set the
connection's `provider` to `anthropic` and keep the line unchanged.

### An opt-in per-run model

The example takes no model in its start input. Add one only when the
application must choose the model per run, because anyone who can start the
Flow can then choose any model, and bill the provider, that the connection
names. Add `Model string` with the JSON name `model` to `SummaryRequest` and
change the same line to:

```go per-run-model
		Model:        cmp.Or(request.Model, flow.summaryModel.Model),
```

## Choose a model per Step

The connector ships a Connector Studio bundle with one configuration unit,
`modelPicker` (`llm.UIUnitModelPicker`, output port
`llm.UIModelPickerPortModel`). Dex Web **Connectors** shows a tab for each
Step that adds it, and renders the same unit for the connection's `model`
field once the connection is saved. The picker reads the saved `provider` and
`region` and lists that provider's live models:

1. a first option that saves an empty pick. On a Step it reads **Connection
   default (claude-opus-5-5)**, naming the connection's `model`, or
   **Connection default (the provider's default model)** when the connection
   has none. In the connection form it reads **Connector default (the
   provider's default model)**;
2. the provider's listed models, with models that cannot serve
   `generateText`, such as embedding or image models, behind **Show all
   models**;
3. a model ID entry, which accepts 1 to 256 printable ASCII characters
   without spaces.

The picker runs only the list commands of the saved provider and region, so
the key reaches only that provider's own hosts, and it never reaches the
browser frame:

| `provider` | Commands | Request |
| --- | --- | --- |
| `openai` | `listOpenAIModels` | `GET https://api.openai.com/v1/models` |
| `anthropic` | `listAnthropicModels` | `GET https://api.anthropic.com/v1/models?limit=1000` with `anthropic-version: 2023-06-01`, paged by `after_id`; Dex Web releases before `cli-v0.13.10` drop the header, so Claude rejects it there |
| `gemini` | `listGeminiModels`, then `listGeminiOpenAICompatibleModels` | `GET https://generativelanguage.googleapis.com/v1beta/models?pageSize=1000` with `x-goog-api-key`, or the OpenAI-compatible list with a bearer key on Dex Web releases before `cli-v0.13.10` |
| `qwen` | `listQwenModels` and `listQwenReasoningModels`, or the `HongKong` pair for `hong-kong` | `GET https://dashscope-intl.aliyuncs.com/api/v1/models` or `https://cn-hongkong.dashscope.aliyuncs.com/api/v1/models` with `capabilities=TG` and `Reasoning`; `china` lists nothing, so type the model ID |
| `deepseek` | `listDeepSeekModels` | `GET https://api.deepseek.com/models` |
| `meta` | `listMetaModels` | `GET https://api.meta.ai/v1/models` |
| `mistral` | `listMistralModels` | `GET https://api.mistral.ai/v1/models`; a regional endpoint serves only its region's subset of this catalog |
| `kimi` | `listKimiModels`, or `listKimiModelsChina` for `china` | `GET https://api.moonshot.ai/v1/models` or `https://api.moonshot.cn/v1/models` |
| `xai` | `listXAIModels`, or `listXAIModelsUS` for `us` | `GET https://api.x.ai/v1/language-models` or `https://us.api.x.ai/v1/models` |

Every command shares the `llm.models-list` capability and sends `api_key`.
A failed list shows a notice and keeps the default option and the model ID
entry. Before the connection is saved, and on Dex Web releases that report no
connection configuration to the frame, the picker cannot know the provider,
so it lists nothing and offers only the default option and the model ID
entry.

The application reads the Step pick and the connection's `model` once at
startup and passes the pick as the request `Model`; restart it after saving
either. The example reads the pick like this:

```go
loaded, err := provider.LoadOperationConfiguration[summarizetext.SummaryModelConfiguration](
	configuration, summarizetext.SummaryModelConfigurationRef(),
)
if errors.Is(err, projectconfig.ErrObjectNotFound) {
	return summarizetext.SummaryModelConfiguration{}, nil
}
```

## Provider reference

Each provider keeps the wire format, model rules, and failure classification
of the connector it replaces. Every streaming provider writes text deltas to
the Step's text Stream as they arrive; Gemini, which does not stream, writes
the whole text once.

### OpenAI (`openai`)

| `GenerateTextRequest` | Responses API |
| --- | --- |
| `Model` | `model` |
| `Instructions` | `instructions` |
| `Messages[]` (`user` or `assistant`) | `input[]` messages with string `content` |
| `StructuredOutput` | `text.format` `json_schema` with `strict: true` |
| `MaxOutputTokens` | `max_output_tokens`, which also counts reasoning tokens |
| `Temperature` (0 to 2; nil omits) | `temperature` |
| `ReasoningEffort` | `reasoning.effort`, with the same value, such as `xhigh` for `ReasoningEffortExtraHigh` |

Every request sends `store: false`, so no stored Response is created; use
the OpenAI connector's `createResponse` for a stored Response. Requests send
`stream: true`, except for `o1-pro`, `o3-pro`, and `gpt-5.5-pro`, whose model
pages do not list streaming. The connector applies the limits each model
page documents, and selects `defect` without a request for a request outside
them:

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

A dated snapshot, such as `gpt-5.4-2026-03-05`, follows its family; a model
the table does not name is sent as written. A fine-tuned `ft:` model receives
`minimum`, `maximum`, length, item, and `format` bounds in descriptions.
`blocked` covers `content_filter`, a refusal, and the
`misalignment_policy_violation`, `bio_policy`, and `cyber_policy` errors. A
429 whose type or code is `insufficient_quota`, `credit_balance_exhausted`, or
a spend or usage limit selects `providerRejected` with `QUOTA_EXHAUSTED`; any
other 429 retries. The final stream event repeats the whole response and may
hold at most 1 MiB.

### Claude (`anthropic`)

| `GenerateTextRequest` | Messages API |
| --- | --- |
| `Model` | `model` |
| `Instructions` | `system` |
| `Messages[]` (`user` or `assistant`) | `messages[]` with string `content` |
| `StructuredOutput` | `output_config.format` `json_schema`; `Description` becomes the schema's root `description`, and bounds Claude rejects move into descriptions |
| `MaxOutputTokens` (zero sends 16000) | `max_tokens`, which includes thinking tokens |
| `Temperature` (nil omits) | `temperature` |
| `ReasoningEffort` | `output_config.effort`: `low`, `medium`, `high`, `xhigh` for `ReasoningEffortExtraHigh`, or `max` |

Every request streams and sends `anthropic-version: 2023-06-01`. Models after
Claude Opus 4.6, and IDs this release does not name, accept only temperature
1.0 and every effort; Opus and Sonnet 4.6 accept 0 to 1 and no `xhigh`; Opus
4.5 accepts `low` through `high`; Sonnet and Haiku 4.5 accept no effort.
`truncated` covers `max_tokens` and `model_context_window_exceeded`, and
`blocked` covers `refusal`. A 402 `billing_error` or the tier spend-cap 429
whose `error.details.error_code` is `enforced_spend_limit_reached` selects
`providerRejected` with `QUOTA_EXHAUSTED`; `overloaded_error`, `api_error`,
`timeout_error`, and other 429s retry, including as stream error events.

### Gemini (`gemini`)

| `GenerateTextRequest` | Gemini API |
| --- | --- |
| `Model` (`gemini-3.8-flash` or `models/gemini-3.8-flash`) | path `models/{model}:generateContent` |
| `Instructions` | `systemInstruction.parts[0].text` |
| `Messages[]` (`user`, or `assistant` sent as `model`) | `contents[]` |
| `StructuredOutput` | `generationConfig.responseJsonSchema` with `responseMimeType: application/json`; `Description` becomes the schema's root description, and `minLength`, `maxLength`, and `format` move into descriptions |
| `MaxOutputTokens` (0 omits) | `generationConfig.maxOutputTokens`, which includes thought tokens |
| `Temperature` (0 to 2; nil omits) | `generationConfig.temperature` |
| `ReasoningEffort` (`minimal`, `low`, `medium`, or `high`) | `generationConfig.thinkingConfig.thinkingLevel` |

Gemini 3.7 and 3.8 Flash, 3 and 3.1 Pro, and the `-latest` aliases accept no
`minimal`; Gemini 1 and 2 models accept no effort. A prompt block reports
`providerFinishReason` `blockReason:SAFETY` and so on and selects `blocked`,
as do the candidate's `SAFETY`, `RECITATION`, `LANGUAGE`, `BLOCKLIST`,
`PROHIBITED_CONTENT`, `SPII`, image-policy, `ESCALATION`, and
`PUP_LIMITED_DISABLED` finishes. A 400 with `API_KEY_INVALID` selects
`providerRejected` with `AUTHENTICATION`. A `RetryInfo` delay replaces
`Retry-After` on a retried 429 or 5xx.

### Qwen (`qwen`)

| `GenerateTextRequest` | Chat Completions |
| --- | --- |
| `Model` | `model` |
| `Instructions` | the first message, with the `system` role |
| `Messages[]` (`user` or `assistant`) | `messages[]` |
| `StructuredOutput` | `response_format` `json_object`, plus a schema instruction in the system message |
| `MaxOutputTokens` | `max_completion_tokens`, which also counts thinking, on Qwen3.5 and later Plus and Flash and Qwen3.7 and later Max models; `max_tokens` on every other model |
| `Temperature` (0 up to but excluding 2; nil omits) | `temperature` |
| `ReasoningEffort` | `reasoning_effort` on `qwen3.8-max`, `qwen3.8-max-0902`, `qwen3.8-flash`, and `qwen3.8-27b`: `none`, `low`, `medium`, or `xhigh`; on `qwen3.8-2.4t-a95b`: `low`, `medium`, or `xhigh` |

Every request streams with `stream_options.include_usage`. Content
moderation, `data_inspection_failed` before or during the stream, selects
`blocked`. `Arrearage`, `AllocationQuota.FreeTierOnly`,
`CommodityNotPurchased`, overdue bills, and `BudgetLimitExceeded` select
`providerRejected` with `QUOTA_EXHAUSTED`. Prefer Qwen3.7 and later models for
structured output; older models may return invalid JSON while thinking.

### DeepSeek (`deepseek`)

| `GenerateTextRequest` | Chat Completions |
| --- | --- |
| `Model` | `model` |
| `Instructions` | the first message, with the `system` role |
| `Messages[]` (`user` or `assistant`) | `messages[]` |
| `StructuredOutput` | `response_format` `json_object`, with the schema appended to the `system` message |
| `MaxOutputTokens` | `max_tokens` |
| `Temperature` | never sent; any value selects `defect` |
| `ReasoningEffort` | `reasoning_effort`: `none` turns thinking off; `low`, `high`, or `max` set the thinking effort |

Every request streams. DeepSeek queues a request for up to 10 minutes with
`: keep-alive` comments, so its exchange may last 1170 seconds and a stream
that sends no byte for 5 minutes retries. The unmapped
`insufficient_system_resource` and `aborted` finishes and empty JSON output
select `invalidResponse`; a 402 insufficient balance selects
`providerRejected` with `QUOTA_EXHAUSTED`. The Receipt's request ID is the
`x-ds-trace-id` header.

### Meta (`meta`)

| `GenerateTextRequest` | Chat Completions |
| --- | --- |
| `Model` | `model` |
| `Instructions` | the first message, with the `developer` role Meta gives the highest precedence |
| `Messages[]` (`user` or `assistant`) | `messages[]` |
| `StructuredOutput` | `response_format` `json_schema` with `strict: true` |
| `MaxOutputTokens` | `max_completion_tokens`, which also counts reasoning tokens |
| `Temperature` (0 to 2; nil omits) | `temperature` |
| `ReasoningEffort` | `reasoning_effort`: `minimal`, `low`, `medium`, `high`, or `xhigh`; `max` only on `muse-spark-1.3` |

Every request streams, which Meta exempts from its non-streaming time limit.
A 400 `content_policy_violation` selects `blocked`; `server_error`,
`server_shutting_down`, `service_overloaded`, `backend_unavailable`, and rate
limit tokens retry, including inside a stream. Contributor-tier models, such
as `muse-spark-1.3-contributor`, let Meta train on prompts and completions.

### Mistral (`mistral`)

| `GenerateTextRequest` | Chat Completions |
| --- | --- |
| `Model` | `model` |
| `Instructions` | the first message, with the `system` role |
| `Messages[]` (`user` or `assistant`) | `messages[]` |
| `StructuredOutput` | `response_format` `json_schema` with `strict: true` |
| `MaxOutputTokens` | `max_tokens`; the prompt plus `max_tokens` cannot exceed the model's context length |
| `Temperature` (0 to 1.5; nil omits) | `temperature` |
| `ReasoningEffort` | `reasoning_effort`: `none` or `high`, only on `mistral-small-2603`, `mistral-medium-3-5`, and their aliases |

Every request streams and sends only the fields Mistral's request schema
accepts. `truncated` covers `length` and `model_length`. Errors use Mistral's
top-level `type` and `code`, and the Receipt's request ID is the
`mistral-correlation-id` header.

### Kimi (`kimi`)

| `GenerateTextRequest` | Chat Completions |
| --- | --- |
| `Model` | `model` |
| `Instructions` | the first message, with the `system` role |
| `Messages[]` (`user` or `assistant`) | `messages[]`, text only |
| `StructuredOutput` | `response_format` `json_schema` with `strict: true`; `title` is removed and bounds move into descriptions |
| `MaxOutputTokens` | `max_completion_tokens`, which also counts thinking tokens |
| `Temperature` | never sent; any value selects `defect` |
| `ReasoningEffort` | `reasoning_effort` on `kimi-k3` models only: `low`, `high`, or `max` |

Every request streams, because Kimi's gateway ends a silent non-streaming
request after 900 seconds. Assistant turns carry no `reasoning_content`, so
use them only on `kimi-k2.6`; on `kimi-k3` and `kimi-k2.7-code`, send one user
turn. A `content_filter` error selects `blocked`;
`exceeded_current_quota_error` selects `providerRejected` with
`QUOTA_EXHAUSTED`; `engine_overloaded_error`, `rate_limit_reached_error`, and
server errors retry. For structured output, prefer `kimi-k2.7-code` or
`kimi-k3` and keep schemas flat on `kimi-k2.6`.

### xAI (`xai`)

| `GenerateTextRequest` | Chat Completions |
| --- | --- |
| `Model` | `model` |
| `Instructions` | the first message, with the `system` role |
| `Messages[]` (`user` or `assistant`) | `messages[]` |
| `StructuredOutput` | `response_format` `json_schema` with `strict: true` |
| `MaxOutputTokens` | `max_completion_tokens`, which bounds visible output only, not reasoning tokens |
| `Temperature` (0 to 2; nil omits) | `temperature` |
| `ReasoningEffort` | `reasoning_effort`: `none` through `xhigh` on `grok-4.3`; `low` through `high` on `grok-4.5`; none on `grok-4.20` and `grok-build-0.1` aliases; `low` through `xhigh` on `grok-4.7`, `grok-4.6`, and other models |

Every request streams. xAI reports `completion_tokens` without reasoning
tokens, so the connector adds them to `Usage.OutputTokens`, as the shared
`Usage` contract states. `end_turn` finishes select `generated`, errors
carry xAI's `code` token, and a stream that carries an error object selects
`invalidResponse`.

## Branches, retry, and failures

`generateText` is a Query with no idempotency key: text generation creates no
provider resource, so a repeated call only bills the tokens again.

| Branch | Required | Selected when |
| --- | --- | --- |
| `generated` | yes | The model finished normally and returned text. |
| `truncated` | no | The model stopped at the output token limit or the context window; `Text` holds any partial output. |
| `blocked` | no | The provider stopped the request or response for a content policy, or the model refused. `Text` is empty. |
| `providerRejected` | no | The provider conclusively rejected the request, such as an invalid key, an unknown model, or exhausted quota or balance (`QUOTA_EXHAUSTED`). |
| `invalidResponse` | no | The response is malformed, larger than `maxResponseBytes`, or has an unknown finish reason, or structured output does not match its schema. |
| `defect` | no | Local input, a request field the model does not accept, the credentials, or the configuration is invalid; no request is sent. |

An unwired optional branch fails the Flow. Transport failures, a stall, an
interrupted stream, HTTP 408, 429 other than quota, and 5xx except 501 return
Retry, honoring `Retry-After`. `Receipt.Provider` and `Failure.Provider` are
the connection's provider, such as `anthropic`. A `Failure` carries only the
status and the provider's bounded error tokens, never the message, prompt,
text, or key. Each Step attempt makes at most one request with the same Call
ID for every attempt of the Step execution; the connector never falls back
to another model. Model failover belongs in the Flow: wire `providerRejected`
or `invalidResponse` to a second llm Step on another connection, or use
`dex.ProceedToOnExecuteFailure`.

Streamed text is written to the Step's text Stream before the finish reason
is known, and a retry does not remove it. After a Retry the Stream can hold
the interrupted attempt's text followed by the whole text of the next
attempt; the Result's `Text` is the only authoritative text.

## Step defaults

| Option | Default |
| --- | --- |
| Execute durability | `sync` |
| Execute timeout | 1200 seconds; one HTTP exchange is bounded at 1170 seconds for DeepSeek, which queues requests for up to 10 minutes, and 870 seconds for every other provider |
| Heartbeat timeout | 60 seconds; the pipeline heartbeats every 5 seconds while a call is in flight |
| Retry | 2-second initial interval, backoff 2, 60-second maximum interval, 4 attempts, 30 minutes total |

`llm.WithHTTPClient` supplies a transport, such as a proxy; the connector uses
a copy that never follows redirects, and `New` rejects a client `Timeout` of
1200 seconds or more. A `StepOptionsOverride` Execute timeout below the
exchange bound lets Dex abandon an exchange that is still running and start
another, which bills twice. `llm.WithBaseURLForTest` points the connection at
a fake provider on `localhost`, `127.0.0.0/8`, or `::1`, such as
`textgentest.FakeProvider`; `New` rejects any other host.

## Migrate from the provider router and the lab connectors

This release is breaking. The package is `llm` instead of `llmrouter`, and
the provider-specific text generation connectors are removed:

| Before | `llm` connection |
| --- | --- |
| `llmrouter` with `openai/gpt-6-sol`, or the OpenAI connector's `generateText` | `provider: openai`, `model: gpt-6-sol` |
| `llmrouter` with `anthropic/...`, or the Claude connector (`connectors/anthropic`) | `provider: anthropic`; its `workspaceId` becomes `anthropicWorkspaceId` |
| `llmrouter` with `gemini/...`, or the Gemini connector's `generateText` | `provider: gemini` |
| Qwen (`connectors/alibaba/qwen`) | `provider: qwen`; its `endpoint` becomes `region` |
| DeepSeek (`connectors/deepseek`) | `provider: deepseek` |
| Meta Model API (`connectors/meta`) | `provider: meta` |
| Mistral AI (`connectors/mistral`) | `provider: mistral`; its `endpoint` becomes `region` |
| Kimi (`connectors/moonshot/kimi`) | `provider: kimi`; its `endpoint` becomes `region` |
| xAI Grok (`connectors/xai`, package `grok`) | `provider: xai`; its `endpoint` becomes `region` |

A provider router connection from an earlier release held several providers
as auth methods with `openai_api_key`, `anthropic_api_key`, and `gemini_api_key`.
A connection now names one provider and holds `api_key`, so save one
connection per provider again; a stored router credential selects `defect`
with `AUTHENTICATION` and no request until it is saved again. Step picks and
request models drop the `provider/` prefix, `QualifiedModel` is removed
because `requestedModel` and `Receipt.Provider` identify the model, and
`Receipt.Provider` is the connection's provider, so Claude reports
`anthropic` and Grok reports `xai`. Claude's `defaultMaxOutputTokens` is
fixed at 16000 and OpenAI's `maxSseEventBytes` at 1 MiB; a request sets
`MaxOutputTokens` to change the first. The labs' custom endpoints are
replaced by the fixed regional hosts above. Applications open connections
with `NewProjectConnection` from the project configuration instead of a
local connections file.

## Example

The [summarize-text example](examples/summarize-text/README.md) runs one Flow
from Dex Web **Start Flow**: it persists the request, asks the picked model
of the connection's provider for a summary while streaming it, and completes
with a generated, truncated, or blocked outcome, or records a rejected,
invalid, or defect outcome and fails.

## Verify

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
npm ci --prefix ../../../sdk/react && npm run build --prefix ../../../sdk/react
npm ci --prefix ui && npm test --prefix ui && npm run build --prefix ui
```

The unit suites run the shared `textgentest` exchange suite on every
provider's wire format, pin each provider's documented request and
classification, and prove each provider's production URL in every region it
serves, its key header, and its default model, without a network call. The
real Dex suites need a running `dexcli dev`:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1
```

An opt-in live test makes one tiny generation with each provider whose key
is set, using that provider's default model, and never prints a key. Set
`LLM_CONNECTOR_TEST_<PROVIDER>_API_KEY`, and optionally `_REGION` and
`_MODEL`, for each provider to test:

```bash
LLM_CONNECTOR_TEST_OPENAI_API_KEY=... LLM_CONNECTOR_TEST_DEEPSEEK_API_KEY=... \
  GOWORK=off go test -tags=live -run TestLive ./...
```
