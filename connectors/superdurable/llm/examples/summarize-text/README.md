# LLM summarize-text example

This example runs one operation-only Flow from Dex Web **Start Flow**:

1. `RecordSummaryRequest` validates the typed start input and persists it in
   the `llm-summary-request` Attribute;
2. `SummarizeText` calls `llmrouter.NewGenerateTextStep` with the Step's
   model pick, and the OpenAI, Claude, or Gemini connector that the pick's
   prefix selects writes the summary to the `llm-summary-text` Stream;
3. `RecordSummaryOutcome` persists the `generated`, `truncated`, or `blocked`
   outcome, with the model as `provider/model`, in the `llm-summary-outcome`
   Attribute and completes the Flow;
4. `RecordSummaryFailure` persists a `providerRejected`, `invalidResponse`,
   or `defect` outcome, with the serving connector and the safe failure
   message, in the `llm-summary-failure` Attribute and fails the Flow with
   that message.

The summary and display RPCs show the request, the outcome, and any failure
in Dex Web. The start input takes no model, so a person who starts the Flow
cannot choose a model or a provider to bill; the connector README shows the
opt-in variant. The request sets no temperature, effort, or output limit, so
every model the picker lists accepts it.

The Flow and every application Step declare stable types with `GetFlowType`
and `GetStepType`, because Dex Web Start Flow sends the type names from the
Flow Definition. The start Step implements a `WaitFor` that skips
immediately, because Dex Web Start Flow invokes the start Step's `WaitFor`.

## Validate the Flow Definition

From `connectors/superdurable/llm`, generate strict FDG 2.0 with the dexcli
release pinned in the repository's `.dex-compat-version` file:

```bash
mkdir -p build
dexcli visualize ./examples/summarize-text/flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/summarize-text
```

The command must report `valid: true`. Inside this repository it also warns
`connector_release_required`, because a local module is not a published
release.

## Configure and run

This example is part of the connector module, so Dex needs release metadata
built from this source, passed as an override; without it the connection
shows **Unsupported**. From the repository root, build the Studio bundle and
the metadata, then start Dex with the generated definition:

```bash
cd "$(git rev-parse --show-toplevel)"
npm ci --prefix sdk/react && npm run build --prefix sdk/react
npm ci --prefix connectors/superdurable/llm/ui && npm run build --prefix connectors/superdurable/llm/ui
mkdir -p /tmp/llm-release
go run ./cmd/connectorctl ui-artifact \
  --manifest connectors/superdurable/llm/connector.yaml \
  --ui-root connectors/superdurable/llm/ui/dist \
  --output /tmp/llm-release/connector-ui.tgz \
  --digest-output /tmp/llm-release/connector-ui.tgz.sha256
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/superdurable/llm/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/superdurable/llm \
  --version v0.1.0 --tag connectors/superdurable/llm/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/llm-release/connector-ui.tgz \
  --ui-digest /tmp/llm-release/connector-ui.tgz.sha256 \
  --output /tmp/llm-release/connector-release.json \
  --digest-output /tmp/llm-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/superdurable/llm/build" \
  --connector-config-dir "$HOME/.dex/connectors" \
  --connector-release-override llm=/tmp/llm-release
```

Open the Dex Web URL that dexcli prints and select **Connections**. Select
`llm / llm`, which the `SummarizeText` Step of `LLMSummarizeText` uses. In the
host-owned form:

- enter the default model in `model` as `provider/model`, such as
  `anthropic/claude-sonnet-5`, or a provider alone, such as `openai`, for that
  provider connector's default model;
- enter the key of each provider you use in `openai_api_key`,
  `anthropic_api_key`, or `gemini_api_key`, following the guide above the
  form, and leave the others blank;
- enter a `wrkspc_` ID in `anthropicWorkspaceId` only for a multi-workspace
  Anthropic key, leave `maxResponseBytes` at its default, and save.

Every save replaces all keys, the model, and the workspace ID, so re-enter
each value you keep. The keys are stored only in the plaintext development
file shown on the page; never commit or share it.

Then open the `generateText · SummarizeText` tab. Its **Summary model** picker
offers each provider's default model and lists the OpenAI, Claude, and Gemini
models your keys reach, each saved as `provider/model`. Pick one, keep the
connection's model, or type `provider/model-id`, and select **Save**. A
provider whose list fails shows a notice; Dex Web releases before
`cli-v0.13.10` cannot list Claude models, so pick **Claude default model** or
type `anthropic/<model-id>` there.

In a second terminal, start the Worker from `connectors/superdurable/llm`
with that file:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/superdurable/llm"
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/summarize-text
```

The Worker reads the connection and the Step's pick at startup, so restart
it after changing either in Dex Web; replaced keys apply without a restart.
It listens on `127.0.0.1:8839`; override `DEX_FLOW_SERVICE_ADDRESS`,
`DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR` when needed. It logs the
connection name and the pick, never a key.

In the Run workspace, choose **Start Flow**, select `LLMSummarizeText`,
choose the Worker at `127.0.0.1:8839`, enter a Flow ID, and submit:

```json
{"text": "The Dex LLM connector routes one generateText Step to OpenAI, Claude, or Gemini and returns typed branches."}
```

The run completes with a `SummaryOutcome` like this, and the run detail
shows the same value under `llm-summary-outcome`:

```json
{
  "branch": "generated",
  "summary": "The Dex LLM connector routes one generateText Step to OpenAI, Claude, or Gemini.",
  "model": "anthropic/claude-sonnet-5",
  "servedModel": "claude-sonnet-5",
  "finishReason": "stop",
  "usage": {"inputTokens": 40, "outputTokens": 90, "totalTokens": 130}
}
```

A pick whose provider has no key fails the run through
`RecordSummaryFailure` with a message such as `summary generation selected
defect: the model selects anthropic, but the connection has no
anthropic_api_key`, before any provider is called. Empty text, or text over
64 KiB, fails the Flow before any provider is called.

## Test

From `connectors/superdurable/llm`:

```bash
GOWORK=off go test -race ./examples/summarize-text/...
```

The real Dex test starts the Flow through the Dex Client against local fake
OpenAI, Claude, and Gemini APIs. It covers the connection model generating
with OpenAI and streaming its text, a Claude pick that is blocked, a Gemini
pick, and a rejected key that records the failure:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/summarize-text/... -count=1
```
