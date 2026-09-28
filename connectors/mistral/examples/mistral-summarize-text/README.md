# Mistral summarize-text example

This example runs one operation-only Flow from Dex Web **Start Flow**:

1. `RecordSummaryRequest` validates the typed start input and persists it in
   the `mistral-summary-request` Attribute;
2. `SummarizeText` calls `mistral.NewGenerateTextStep`, which streams the
   summary to the `mistral-summary-text` Stream as the Mistral model writes
   it;
3. `RecordSummaryOutcome` persists the `generated`, `truncated`, or `blocked`
   outcome in the `mistral-summary-outcome` Attribute and completes the Flow.

The summary and display RPCs show the request and the outcome in Dex Web.
The example leaves `providerRejected`, `invalidResponse`, and `defect`
unwired, so those outcomes fail the Flow. For example, a picked model that
the connection's regional endpoint does not serve selects `providerRejected`
and fails the run without a retry.

The request sets no reasoning effort, because Mistral accepts one only on
its adjustable-reasoning models, and the default `mistral-large-2512` is not
one of them. Its 1024-token limit also covers a thinking trace when a Step
picks a reasoning model.

The Flow and every application Step declare stable types with `GetFlowType`
and `GetStepType`, because Dex Web Start Flow sends the type names from the
Flow Definition. The start Step implements a `WaitFor` that skips
immediately, because Dex Web Start Flow invokes the start Step's `WaitFor`.

## Validate the Flow Definition

From `connectors/mistral`, generate strict FDG 2.0 with the dexcli release
pinned in the repository's `.dex-compat-version` file:

```bash
mkdir -p build
dexcli visualize ./examples/mistral-summarize-text/flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/mistral-summarize-text
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
npm ci --prefix connectors/mistral/ui && npm run build --prefix connectors/mistral/ui
mkdir -p /tmp/mistral-release
go run ./cmd/connectorctl ui-artifact \
  --manifest connectors/mistral/connector.yaml \
  --ui-root connectors/mistral/ui/dist \
  --output /tmp/mistral-release/connector-ui.tgz \
  --digest-output /tmp/mistral-release/connector-ui.tgz.sha256
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/mistral/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/mistral \
  --version v0.1.0 --tag connectors/mistral/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/mistral-release/connector-ui.tgz \
  --ui-digest /tmp/mistral-release/connector-ui.tgz.sha256 \
  --output /tmp/mistral-release/connector-release.json \
  --digest-output /tmp/mistral-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/mistral/build" \
  --connector-config-dir "$HOME/.dex/connectors" \
  --connector-release-override mistral=/tmp/mistral-release
```

Open the Dex Web URL that dexcli prints and select **Connections**. Select
`mistral / mistral-api`, which the `SummarizeText` Step of
`MistralSummarizeText` uses. In the host-owned form, enter a Mistral API key
in `api_key`, leave `model` blank for `mistral-large-2512` or enter another
model, leave `endpoint` blank for the global endpoint or enter
`https://api.eu.mistral.ai/v1` or `https://api.us.mistral.ai/v1`, leave
`maxResponseBytes` at its default, and save. The status becomes **Ready**.

Then open the `generateText · SummarizeText` tab. Its **Summary model** picker
lists Mistral's models live. Pick one, keep the connection's model, or type a
model ID, and select **Save**. The key is stored only in the plaintext
development file shown on the page; never commit or share it.

In a second terminal, start the Worker from `connectors/mistral` with that
file:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/mistral"
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/mistral-summarize-text
```

The Worker reads the connection and the Step's pick at startup, so restart
it after changing either in Dex Web. It listens on `127.0.0.1:8824`; override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed. It logs the connection name, never the key.

In the Run workspace, choose **Start Flow**, select `MistralSummarizeText`,
choose the Worker at `127.0.0.1:8824`, enter a Flow ID, and submit:

```json
{"text": "The Dex Mistral connector calls Mistral AI Chat Completions, streams the text, and returns typed branches."}
```

The run completes with a `SummaryOutcome` like this, and the run detail
shows the same value under `mistral-summary-outcome`:

```json
{
  "branch": "generated",
  "summary": "The Dex Mistral connector calls Mistral AI Chat Completions and streams the text.",
  "servedModel": "mistral-large-2512",
  "finishReason": "stop",
  "usage": {"inputTokens": 45, "outputTokens": 18, "totalTokens": 63}
}
```

Empty text, or text over 64 KiB, fails the Flow before Mistral is called.

## Test

From `connectors/mistral`:

```bash
GOWORK=off go test -race ./examples/mistral-summarize-text/...
```

The real Dex test starts the Flow through the Dex Client against a local
fake Mistral API. It covers the generated route with its streamed text, the
truncated route for a `model_length` finish, and the blocked route:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/mistral-summarize-text/... -count=1
```
