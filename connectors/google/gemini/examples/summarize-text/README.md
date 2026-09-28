# Gemini summarize-text example

This example runs one operation-only Flow from Dex Web **Start Flow**:

1. `RecordSummaryRequest` validates the typed start input and persists it in
   the `gemini-text-summary-request` Attribute;
2. `SummarizeText` calls `gemini.NewGenerateTextStep`, the provider-neutral
   `generateText` Query, which writes the summary to the
   `gemini-text-summary-text` Stream when Gemini answers;
3. `RecordSummaryOutcome` persists the `generated`, `truncated`, or `blocked`
   outcome in the `gemini-text-summary-outcome` Attribute and completes the
   Flow.

The summary and display RPCs show the request and the outcome in Dex Web.
The example leaves `providerRejected`, `invalidResponse`, and `defect`
unwired, so those outcomes fail the Flow. For example, a Gemini project
without prepay credits returns HTTP 402, which selects `providerRejected` and
fails the run without a retry.

The Flow and every application Step declare stable types with `GetFlowType`
and `GetStepType`, because Dex Web Start Flow sends the type names from the
Flow Definition. The start Step implements a `WaitFor` that skips
immediately, because Dex Web Start Flow invokes the start Step's `WaitFor`.

## Validate the Flow Definition

From `connectors/google/gemini`, generate strict FDG 2.0 with the dexcli
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
npm ci --prefix connectors/google/gemini/ui && npm run build --prefix connectors/google/gemini/ui
mkdir -p /tmp/gemini-release
go run ./cmd/connectorctl ui-artifact \
  --manifest connectors/google/gemini/connector.yaml \
  --ui-root connectors/google/gemini/ui/dist \
  --output /tmp/gemini-release/connector-ui.tgz \
  --digest-output /tmp/gemini-release/connector-ui.tgz.sha256
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/google/gemini/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/google/gemini \
  --version v0.3.0 --tag connectors/google/gemini/v0.3.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/gemini-release/connector-ui.tgz \
  --ui-digest /tmp/gemini-release/connector-ui.tgz.sha256 \
  --output /tmp/gemini-release/connector-release.json \
  --digest-output /tmp/gemini-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/google/gemini/build" \
  --connector-config-dir "$HOME/.dex/connectors" \
  --connector-release-override gemini=/tmp/gemini-release
```

Open the Dex Web URL that dexcli prints and select **Connections**. Select
`gemini / gemini-api`, which the `SummarizeText` Step of `GeminiSummarizeText`
uses. In the host-owned form, enter a Gemini API key from Google AI Studio in
`api_key`, leave `model` blank for `gemini-3.5-flash-lite` or enter another
model such as `gemini-3.8-flash`, leave `endpoint` and `maxResponseBytes` at
their defaults, and save. The status becomes **Ready**.

Then open the `generateText · SummarizeText` tab. Its **Summary model** picker
lists Gemini's models live. Pick one, keep the connection's model, or type a
model ID, and select **Save**. The key is stored only in the plaintext
development file shown on the page; never commit or share it.

In a second terminal, start the Worker from `connectors/google/gemini` with
that file:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/google/gemini"
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/summarize-text
```

The Worker reads the connection and the Step's pick at startup, so restart
it after changing either in Dex Web. It listens on `127.0.0.1:8825`; override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed. It logs the connection name, never the key.

In the Run workspace, choose **Start Flow**, select `GeminiSummarizeText`,
choose the Worker at `127.0.0.1:8825`, enter a Flow ID, and submit:

```json
{"text": "The Dex Gemini connector calls models.generateContent through the shared generateText Query and returns typed branches."}
```

The run completes with a `SummaryOutcome` like this, and the run detail
shows the same value under `gemini-text-summary-outcome`:

```json
{
  "branch": "generated",
  "summary": "The Dex Gemini connector calls models.generateContent and returns typed branches.",
  "servedModel": "gemini-3.5-flash-lite",
  "finishReason": "stop",
  "providerFinishReason": "STOP",
  "usage": {"inputTokens": 40, "outputTokens": 20, "totalTokens": 60}
}
```

Empty text, or text over 64 KiB, fails the Flow before Gemini is called.

## Test

From `connectors/google/gemini`:

```bash
GOWORK=off go test -race ./examples/summarize-text/...
```

The real Dex test starts the Flow through the Dex Client against a local
fake Gemini API. It covers the generated route with its text Stream and a
blocked prompt:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/summarize-text/... -count=1
```
