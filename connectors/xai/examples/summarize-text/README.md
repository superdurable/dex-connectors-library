# Grok summarize-text example

This example runs one operation-only Flow from Dex Web **Start Flow**:

1. `RecordSummaryRequest` validates the typed start input and persists it in
   the `grok-summary-request` Attribute;
2. `SummarizeText` calls `grok.NewGenerateTextStep`, which streams the
   summary to the `grok-summary-text` Stream as Grok writes it;
3. `RecordSummaryOutcome` persists the `generated`, `truncated`, or `blocked`
   outcome in the `grok-summary-outcome` Attribute and completes the Flow.

The summary and display RPCs show the request and the outcome in Dex Web.
The example leaves `providerRejected`, `invalidResponse`, and `defect`
unwired, so those outcomes fail the Flow. For example, an incorrect API key
gets HTTP 400 `invalid-argument`, which selects `providerRejected` and fails
the run without a retry.

The request leaves reasoning effort to each model's default, because Grok
4.20 and Grok Build accept no `reasoning_effort`; any model the picker lists
then works.

The Flow and every application Step declare stable types with `GetFlowType`
and `GetStepType`, because Dex Web Start Flow sends the type names from the
Flow Definition. The start Step implements a `WaitFor` that skips
immediately, because Dex Web Start Flow invokes the start Step's `WaitFor`.

## Validate the Flow Definition

From `connectors/xai`, generate strict FDG 2.0 with the dexcli release
pinned in the repository's `.dex-compat-version` file:

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
npm ci --prefix connectors/xai/ui && npm run build --prefix connectors/xai/ui
mkdir -p /tmp/grok-release
go run ./cmd/connectorctl ui-artifact \
  --manifest connectors/xai/connector.yaml \
  --ui-root connectors/xai/ui/dist \
  --output /tmp/grok-release/connector-ui.tgz \
  --digest-output /tmp/grok-release/connector-ui.tgz.sha256
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/xai/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/xai \
  --version v0.1.0 --tag connectors/xai/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/grok-release/connector-ui.tgz \
  --ui-digest /tmp/grok-release/connector-ui.tgz.sha256 \
  --output /tmp/grok-release/connector-release.json \
  --digest-output /tmp/grok-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/xai/build" \
  --connector-config-dir "$HOME/.dex/connectors" \
  --connector-release-override grok=/tmp/grok-release
```

Open the Dex Web URL that dexcli prints and select **Connections**. Select
`grok / grok-api`, which the `SummarizeText` Step of `GrokSummarizeText`
uses. In the host-owned form, enter an xAI API key in `api_key`, leave
`model` blank for `grok-4.3` or enter another model, leave `endpoint` at
`https://api.x.ai/v1` or enter `https://us.api.x.ai/v1` together with
`grok-4.7` or `grok-4.6`, leave `maxResponseBytes` at its default, and save.
The status becomes **Ready**.

Then open the `generateText · SummarizeText` tab. Its **Summary model** picker
lists xAI's models live. Pick one, keep the connection's model, or type a
model ID, and select **Save**. The key is stored only in the plaintext
development file shown on the page; never commit or share it.

In a second terminal, start the Worker from `connectors/xai` with that file:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/xai"
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/summarize-text
```

The Worker reads the connection and the Step's pick at startup, so restart
it after changing either in Dex Web. It listens on `127.0.0.1:8817`; override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed. It logs the connection name, never the key.

In the Run workspace, choose **Start Flow**, select `GrokSummarizeText`,
choose the Worker at `127.0.0.1:8817`, enter a Flow ID, and submit:

```json
{"text": "The Dex Grok connector calls xAI Chat Completions, streams the text, and returns typed branches."}
```

The run completes with a `SummaryOutcome` like this, and the run detail
shows the same value under `grok-summary-outcome`:

```json
{
  "branch": "generated",
  "summary": "The Dex Grok connector calls xAI Chat Completions and streams the text.",
  "servedModel": "grok-4.3",
  "finishReason": "stop",
  "usage": {"inputTokens": 40, "outputTokens": 88, "reasoningTokens": 70, "totalTokens": 128}
}
```

`outputTokens` includes the `reasoningTokens`, as on every lab connector,
although xAI reports them apart from `completion_tokens`; `totalTokens` is
the billed total. Empty text, or text over 64 KiB, fails the Flow before
xAI is called.

## Test

From `connectors/xai`:

```bash
GOWORK=off go test -race ./examples/summarize-text/...
```

The real Dex test starts the Flow through the Dex Client against a local
fake xAI API. It covers the generated route with its streamed text and the
blocked route:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/summarize-text/... -count=1
```
