# Qwen summarize-text example

This example runs one operation-only Flow from Dex Web **Start Flow**:

1. `RecordSummaryRequest` validates the typed start input and persists it in
   the `qwen-summary-request` Attribute;
2. `SummarizeText` calls `qwen.NewGenerateTextStep`, which streams the
   summary to the `qwen-summary-text` Stream as the Qwen model writes it;
3. `RecordSummaryOutcome` persists the `generated`, `truncated`, or `blocked`
   outcome in the `qwen-summary-outcome` Attribute and completes the Flow.

The summary and display RPCs show the request and the outcome in Dex Web.
The example leaves `providerRejected`, `invalidResponse`, and `defect`
unwired, so those outcomes fail the Flow. For example, an account with an
overdue payment gets HTTP 400 `Arrearage`, which selects `providerRejected`
and fails the run without a retry.

The Flow and every application Step declare stable types with `GetFlowType`
and `GetStepType`, because Dex Web Start Flow sends the type names from the
Flow Definition. The start Step implements a `WaitFor` that skips
immediately, because Dex Web Start Flow invokes the start Step's `WaitFor`.

## Validate the Flow Definition

From `connectors/alibaba/qwen`, generate strict FDG 2.0 with the dexcli
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
npm ci --prefix connectors/alibaba/qwen/ui && npm run build --prefix connectors/alibaba/qwen/ui
mkdir -p /tmp/qwen-release
go run ./cmd/connectorctl ui-artifact \
  --manifest connectors/alibaba/qwen/connector.yaml \
  --ui-root connectors/alibaba/qwen/ui/dist \
  --output /tmp/qwen-release/connector-ui.tgz \
  --digest-output /tmp/qwen-release/connector-ui.tgz.sha256
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/alibaba/qwen/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/alibaba/qwen \
  --version v0.1.0 --tag connectors/alibaba/qwen/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/qwen-release/connector-ui.tgz \
  --ui-digest /tmp/qwen-release/connector-ui.tgz.sha256 \
  --output /tmp/qwen-release/connector-release.json \
  --digest-output /tmp/qwen-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/alibaba/qwen/build" \
  --connector-config-dir "$HOME/.dex/connectors" \
  --connector-release-override qwen=/tmp/qwen-release
```

Open the Dex Web URL that dexcli prints and select **Connections**. Select
`qwen / qwen-api`, which the `SummarizeText` Step of `QwenSummarizeText`
uses. In the host-owned form, enter a Model Studio API key in `api_key`,
leave `endpoint` blank for Singapore or enter the DashScope endpoint of the
key's region, leave `model` blank for `qwen3.7-plus` or enter another model,
leave `maxResponseBytes` at its default, and save. The status becomes
**Ready**.

Then open the `generateText · SummarizeText` tab. Its **Summary model** picker
lists Model Studio's text models live from Singapore or China (Hong Kong).
Pick one, keep the connection's model, or type a model ID, and select
**Save**. The key is stored only in the plaintext development file shown on
the page; never commit or share it.

In a second terminal, start the Worker from `connectors/alibaba/qwen` with
that file:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/alibaba/qwen"
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/summarize-text
```

The Worker reads the connection and the Step's pick at startup, so restart
it after changing either in Dex Web. It listens on `127.0.0.1:8820`; override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed. It logs the connection name, never the key.

In the Run workspace, choose **Start Flow**, select `QwenSummarizeText`,
choose the Worker at `127.0.0.1:8820`, enter a Flow ID, and submit:

```json
{"text": "The Dex Qwen connector calls Alibaba Cloud Model Studio Chat Completions, streams the text, and returns typed branches."}
```

The run completes with a `SummaryOutcome` like this, and the run detail
shows the same value under `qwen-summary-outcome`:

```json
{
  "branch": "generated",
  "summary": "The Dex Qwen connector calls Model Studio Chat Completions and streams the text.",
  "servedModel": "qwen3.7-plus",
  "finishReason": "stop",
  "usage": {"inputTokens": 45, "outputTokens": 310, "reasoningTokens": 290, "totalTokens": 355}
}
```

Empty text, or text over 64 KiB, fails the Flow before Model Studio is
called.

## Test

From `connectors/alibaba/qwen`:

```bash
GOWORK=off go test -race ./examples/summarize-text/...
```

The real Dex test starts the Flow through the Dex Client against a local
fake Model Studio API. It covers the generated route with its streamed text
and the content-moderation route that selects `blocked`:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/summarize-text/... -count=1
```
