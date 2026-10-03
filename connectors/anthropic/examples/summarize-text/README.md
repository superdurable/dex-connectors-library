# Claude summarize-text example

This example runs one operation-only Flow from Dex Web **Start Flow**:

1. `RecordSummaryRequest` validates the typed start input and persists it in
   the `claude-summary-request` Attribute;
2. `SummarizeText` calls `claude.NewGenerateTextStep`, which streams the
   summary to the `claude-summary-text` Stream as Claude writes it;
3. `RecordSummaryOutcome` persists the `generated`, `truncated`, or `blocked`
   outcome in the `claude-summary-outcome` Attribute and completes the Flow.

The summary and display RPCs show the request and the outcome in Dex Web.
The example leaves `providerRejected`, `invalidResponse`, and `defect`
unwired, so those outcomes fail the Flow. For example, an organization with a
billing issue gets HTTP 402, which selects `providerRejected` and fails the
run without a retry.

The request sets no `ReasoningEffort`, so every model the picker lists
accepts it, including Claude Haiku 4.5, which has no effort levels. Its
8192-token `MaxOutputTokens` leaves room for thinking, which counts toward
Claude's `max_tokens`.

The Flow and every application Step declare stable types with `GetFlowType`
and `GetStepType`, because Dex Web Start Flow sends the type names from the
Flow Definition. The start Step implements a `WaitFor` that skips
immediately, because Dex Web Start Flow invokes the start Step's `WaitFor`.

## Validate the Flow Definition

From `connectors/anthropic`, generate strict FDG 2.0 with the latest stable
dexcli release:

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
npm ci --prefix connectors/anthropic/ui && npm run build --prefix connectors/anthropic/ui
mkdir -p /tmp/claude-release
go run ./cmd/connectorctl ui-artifact \
  --manifest connectors/anthropic/connector.yaml \
  --ui-root connectors/anthropic/ui/dist \
  --output /tmp/claude-release/connector-ui.tgz \
  --digest-output /tmp/claude-release/connector-ui.tgz.sha256
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/anthropic/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/anthropic \
  --version v0.1.0 --tag connectors/anthropic/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/claude-release/connector-ui.tgz \
  --ui-digest /tmp/claude-release/connector-ui.tgz.sha256 \
  --output /tmp/claude-release/connector-release.json \
  --digest-output /tmp/claude-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/anthropic/build" \
  --connector-config-dir "$HOME/.dex/connectors" \
  --connector-release-override claude=/tmp/claude-release
```

Open the Dex Web URL that dexcli prints and select **Connections**. Select
`claude / claude-api`, which the `SummarizeText` Step of
`ClaudeSummarizeText` uses. In the host-owned form, enter a Claude API key in
`api_key`, leave `model` blank for `claude-sonnet-5` or enter another model,
enter a `wrkspc_` ID in `workspaceId` only for a multi-workspace key, leave
the other fields at their defaults, and save. The status becomes **Ready**.

Then open the `generateText · SummarizeText` tab. Its **Summary model** picker
lists Claude's models live. Pick one, keep the connection's model, or type a
model ID, and select **Save**. Dex Web `cli-v0.13.8` and earlier do not send
the list's `anthropic-version` header, so there the list fails and the
picker offers manual model ID entry. The key is stored only in the plaintext
development file shown on the page; never commit or share it.

In a second terminal, start the Worker from `connectors/anthropic` with that
file:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/anthropic"
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/summarize-text
```

The Worker reads the connection and the Step's pick at startup, so restart
it after changing either in Dex Web. It listens on `127.0.0.1:8819`; override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed. It logs the connection name, never the key.

In the Run workspace, choose **Start Flow**, select `ClaudeSummarizeText`,
choose the Worker at `127.0.0.1:8819`, enter a Flow ID, and submit:

```json
{"text": "The Dex Claude connector calls the Claude Messages API, streams the text, and returns typed branches."}
```

The run completes with a `SummaryOutcome` like this, and the run detail
shows the same value under `claude-summary-outcome`:

```json
{
  "branch": "generated",
  "summary": "The Dex Claude connector calls the Claude Messages API and streams the text.",
  "servedModel": "claude-sonnet-5",
  "finishReason": "stop",
  "usage": {"inputTokens": 40, "outputTokens": 90, "totalTokens": 130}
}
```

Empty text, or text over 64 KiB, fails the Flow before Claude is called.

## Test

From `connectors/anthropic`:

```bash
GOWORK=off go test -race ./examples/summarize-text/...
```

The real Dex test starts the Flow through the Dex Client against a local
fake Claude API. It covers the generated route with its streamed text and
the blocked route:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/summarize-text/... -count=1
```
