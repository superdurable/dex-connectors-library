# DeepSeek summarize-text example

This example runs one operation-only Flow from Dex Web **Start Flow**:

1. `RecordSummaryRequest` validates the typed start input and persists it in
   the `deepseek-summary-request` Attribute;
2. `SummarizeText` calls `deepseek.NewGenerateTextStep` with thinking off,
   which streams the summary to the `deepseek-summary-text` Stream as the
   model writes it;
3. `RecordSummaryOutcome` persists the `generated`, `truncated`, or `blocked`
   outcome in the `deepseek-summary-outcome` Attribute and completes the Flow.

The summary and display RPCs show the request and the outcome in Dex Web.
The example leaves `providerRejected`, `invalidResponse`, and `defect`
unwired, so those outcomes fail the Flow. For example, an account with an
exhausted balance gets HTTP 402, which selects `providerRejected` and fails
the run without a retry.

The Flow and every application Step declare stable types with `GetFlowType`
and `GetStepType`, because Dex Web Start Flow sends the type names from the
Flow Definition. The start Step implements a `WaitFor` that skips
immediately, because Dex Web Start Flow invokes the start Step's `WaitFor`.

## Validate the Flow Definition

From `connectors/deepseek`, generate strict FDG 2.0 with the dexcli release
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
npm ci --prefix connectors/deepseek/ui && npm run build --prefix connectors/deepseek/ui
mkdir -p /tmp/deepseek-release
go run ./cmd/connectorctl ui-artifact \
  --manifest connectors/deepseek/connector.yaml \
  --ui-root connectors/deepseek/ui/dist \
  --output /tmp/deepseek-release/connector-ui.tgz \
  --digest-output /tmp/deepseek-release/connector-ui.tgz.sha256
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/deepseek/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/deepseek \
  --version v0.1.0 --tag connectors/deepseek/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/deepseek-release/connector-ui.tgz \
  --ui-digest /tmp/deepseek-release/connector-ui.tgz.sha256 \
  --output /tmp/deepseek-release/connector-release.json \
  --digest-output /tmp/deepseek-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/deepseek/build" \
  --connector-config-dir "$HOME/.dex/connectors" \
  --connector-release-override deepseek=/tmp/deepseek-release
```

Open the Dex Web URL that dexcli prints and select **Connections**. Select
`deepseek / deepseek-api`, which the `SummarizeText` Step of
`DeepSeekSummarizeText` uses. In the host-owned form, enter a DeepSeek API key
in `api_key`, leave `model` blank for `deepseek-flash` or enter another model,
leave `maxResponseBytes` at its default, and save. The status becomes
**Ready**.

Then open the `generateText · SummarizeText` tab. Its **Summary model** picker
lists DeepSeek's models live. Pick one, keep the connection's model, or type
a model ID, and select **Save**. The key is stored only in the plaintext
development file shown on the page; never commit or share it.

In a second terminal, start the Worker from `connectors/deepseek` with that
file:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/deepseek"
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/summarize-text
```

The Worker reads the connection and the Step's pick at startup, so restart
it after changing either in Dex Web. It listens on `127.0.0.1:8818`; override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed. It logs the connection name, never the key.

In the Run workspace, choose **Start Flow**, select `DeepSeekSummarizeText`,
choose the Worker at `127.0.0.1:8818`, enter a Flow ID, and submit:

```json
{"text": "The Dex DeepSeek connector calls DeepSeek Chat Completions, streams the text, and returns typed branches."}
```

The run completes with a `SummaryOutcome` like this, and the run detail
shows the same value under `deepseek-summary-outcome`:

```json
{
  "branch": "generated",
  "summary": "The Dex DeepSeek connector calls DeepSeek Chat Completions and streams the text.",
  "servedModel": "deepseek-flash",
  "finishReason": "stop",
  "usage": {"inputTokens": 40, "outputTokens": 20, "totalTokens": 60}
}
```

Empty text, or text over 64 KiB, fails the Flow before DeepSeek is called.
When DeepSeek is busy, the Step can wait up to 10 minutes while DeepSeek
queues the request; the run shows the Step running meanwhile.

## Test

From `connectors/deepseek`:

```bash
GOWORK=off go test -race ./examples/summarize-text/...
```

The real Dex test starts the Flow through the Dex Client against a local
fake DeepSeek API. It covers the generated route with its streamed text, the
blocked route, an exhausted balance that fails the Flow without a retry, and
empty text that fails before DeepSeek is called:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/summarize-text/... -count=1
```
