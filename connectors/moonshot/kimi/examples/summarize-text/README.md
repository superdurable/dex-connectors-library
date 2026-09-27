# Kimi summarize-text example

This example runs one operation-only Flow from Dex Web **Start Flow**:

1. `RecordSummaryRequest` validates the typed start input and persists it in
   the `kimi-summary-request` Attribute;
2. `SummarizeText` calls `kimi.NewGenerateTextStep`, which streams the
   summary to the `kimi-summary-text` Stream as the Kimi model writes it;
3. `RecordSummaryOutcome` persists the `generated`, `truncated`, or `blocked`
   outcome in the `kimi-summary-outcome` Attribute and completes the Flow.

The summary and display RPCs show the request and the outcome in Dex Web.
The example leaves `providerRejected`, `invalidResponse`, and `defect`
unwired, so those outcomes fail the Flow. For example, an account with an
exhausted balance gets HTTP 429 `exceeded_current_quota_error`, which selects
`providerRejected` and fails the run without a retry, and a key from the
other Kimi platform gets HTTP 401, which does the same.

The request sets no `Temperature` or `ReasoningEffort`, so it runs on every
Kimi model the picker offers. Its 16384-token output limit leaves room for
the thinking tokens that `kimi-k2.6` spends by default.

The Flow and every application Step declare stable types with `GetFlowType`
and `GetStepType`, because Dex Web Start Flow sends the type names from the
Flow Definition. The start Step implements a `WaitFor` that skips
immediately, because Dex Web Start Flow invokes the start Step's `WaitFor`.

## Validate the Flow Definition

From `connectors/moonshot/kimi`, generate strict FDG 2.0 with the dexcli
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
npm ci --prefix connectors/moonshot/kimi/ui && npm run build --prefix connectors/moonshot/kimi/ui
mkdir -p /tmp/kimi-release
go run ./cmd/connectorctl ui-artifact \
  --manifest connectors/moonshot/kimi/connector.yaml \
  --ui-root connectors/moonshot/kimi/ui/dist \
  --output /tmp/kimi-release/connector-ui.tgz \
  --digest-output /tmp/kimi-release/connector-ui.tgz.sha256
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/moonshot/kimi/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/moonshot/kimi \
  --version v0.1.0 --tag connectors/moonshot/kimi/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/kimi-release/connector-ui.tgz \
  --ui-digest /tmp/kimi-release/connector-ui.tgz.sha256 \
  --output /tmp/kimi-release/connector-release.json \
  --digest-output /tmp/kimi-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/moonshot/kimi/build" \
  --connector-config-dir "$HOME/.dex/connectors" \
  --connector-release-override kimi=/tmp/kimi-release
```

Open the Dex Web URL that dexcli prints and select **Connections**. Select
`kimi / kimi-api`, which the `SummarizeText` Step of `KimiSummarizeText`
uses. In the host-owned form, enter a Kimi API key in `api_key`, leave
`model` blank for `kimi-k2.6` or enter another model, leave `endpoint` blank
for a `platform.kimi.ai` key or enter `https://api.moonshot.cn/v1` for a
`platform.kimi.com` key, leave `maxResponseBytes` at its default, and save.
The status becomes **Ready**.

Then open the `generateText · SummarizeText` tab. Its **Summary model** picker
lists Kimi's models live from whichever platform accepts the key. Pick one,
keep the connection's model, or type a model ID, and select **Save**. The key
is stored only in the plaintext development file shown on the page; never
commit or share it.

In a second terminal, start the Worker from `connectors/moonshot/kimi` with
that file:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/moonshot/kimi"
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/summarize-text
```

The Worker reads the connection and the Step's pick at startup, so restart
it after changing either in Dex Web. It listens on `127.0.0.1:8820`; override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed. It logs the connection name, never the key.

In the Run workspace, choose **Start Flow**, select `KimiSummarizeText`,
choose the Worker at `127.0.0.1:8820`, enter a Flow ID, and submit:

```json
{"text": "The Dex Kimi connector calls Kimi API Chat Completions, streams the text, and returns typed branches."}
```

The run completes with a `SummaryOutcome` like this, and the run detail
shows the same value under `kimi-summary-outcome`:

```json
{
  "branch": "generated",
  "summary": "The Dex Kimi connector calls Kimi API Chat Completions and streams the text.",
  "servedModel": "kimi-k2.6",
  "finishReason": "stop",
  "usage": {"inputTokens": 40, "outputTokens": 160, "totalTokens": 200}
}
```

Empty text, or text over 64 KiB, fails the Flow before Kimi is called.

## Test

From `connectors/moonshot/kimi`:

```bash
GOWORK=off go test -race ./examples/summarize-text/...
```

The real Dex test starts the Flow through the Dex Client against a local
fake Kimi API. It covers the generated route with its streamed text, a 400
`content_filter` rejection that completes as blocked, and a 429
`exceeded_current_quota_error` that fails the Flow without a retry:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/summarize-text/... -count=1
```
