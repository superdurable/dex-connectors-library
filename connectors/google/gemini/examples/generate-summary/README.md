# Gemini generate-summary example

This example runs one operation-only Flow from Dex Web **Start Flow**:

1. `RecordSummaryRequest` validates the typed start input and persists it in the
   `gemini-summary-request` Attribute;
2. `GenerateSummary` calls `gemini.NewGenerateContentStep` with a JSON Schema,
   so Gemini returns a structured headline, summary, and key points;
3. `SummaryGenerated` decodes that JSON and completes the Flow, while
   `SummaryNotGenerated` completes without a summary when Gemini truncated or
   blocked the candidate.

Both completion Steps write the `gemini-summary-outcome` Attribute. The
summary and display RPCs show the request and the outcome in Dex Web.

The example wires `generated`, `truncated`, and `blocked`. It leaves
`providerRejected`, `invalidResponse`, and `defect` unwired, so those outcomes
fail the Flow. For example, a Gemini project without prepay credits returns
HTTP 402, which selects `providerRejected` and fails the run without a retry.

The Flow and every application Step declare stable types with `GetFlowType`
and `GetStepType`. Dex Web Start Flow sends the type names from the Flow
Definition, and the Worker must register the same names. The start Step
implements a `WaitFor` that skips immediately because Dex Web Start Flow
invokes the start Step's `WaitFor`.

## Validate the Flow Definition

From `connectors/google/gemini`, generate strict FDG 2.0 with the dexcli
release pinned in the repository's `.dex-compat-version` file:

```bash
mkdir -p build
dexcli visualize ./examples/generate-summary/flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/generate-summary
```

The command must report `valid: true`. Inside this repository it also warns
`connector_release_required`, because a local module is not a published
release. A consumer module that requires a published Gemini release, or the
compatibility gate's temporary module proxy, resolves the exact module version.

## Configure and run

This example is part of the connector module, so its Flow Definition names the
local module rather than a published release. Dex needs release metadata built
from this source, passed as an override; without it the connection shows
**Unsupported** and cannot be configured, even after the release is
published. From the repository root, build the metadata and start Dex with the
generated definition:

```bash
cd "$(git rev-parse --show-toplevel)"
mkdir -p /tmp/gemini-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/google/gemini/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/google/gemini \
  --version v0.1.0 --tag connectors/google/gemini/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/gemini-release/connector-release.json \
  --digest-output /tmp/gemini-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/google/gemini/build" \
  --connector-config-dir "$HOME/.dex/connectors" \
  --connector-release-override gemini=/tmp/gemini-release
```

Dex Web shows **Local override** for the connection. A consumer module that
requires a published Gemini release omits `--connector-release-override`.

Open the Dex Web URL that dexcli prints and select **Connections**. Select
`gemini / gemini-api`, which is used by the `GenerateSummary` Step of
`GeminiGenerateSummary`. In the host-owned form, enter a Gemini API key from
Google AI Studio in `api_key`, leave `endpoint` and `maxResponseBytes` at their
defaults, and save. The status becomes **Ready**. The key is stored only in the
plaintext development file shown on the page; never commit or share it.

In a second terminal, start the Worker from `connectors/google/gemini` with
that file:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/google/gemini"
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/generate-summary
```

The Worker calls `gemini-3.5-flash-lite`, which a new Google AI Studio project
can use. Set `GEMINI_SUMMARY_MODEL`, such as `gemini-3.8-flash`, to call
another model. The Worker listens on `127.0.0.1:8815`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed. If the Dex Server is unreachable, the Worker logs `dex server
unavailable; retrying` and starts once Dex answers. It logs the connection name
and model, never the key.

In the Run workspace, choose **Start Flow**, select `GeminiGenerateSummary`,
choose the Worker at `127.0.0.1:8815`, enter a Flow ID, and submit this input:

```json
{
  "title": "Gemini connector",
  "text": "The Dex Gemini connector calls models.generateContent with JSON Schema structured output and returns typed branches."
}
```

The run completes with a `SummaryOutcome` like this, and the run detail shows
the same value under `gemini-summary-outcome`:

```json
{
  "status": "generated",
  "title": "Gemini connector",
  "summary": {
    "headline": "Gemini connector ships",
    "summary": "Dex Flows can call Gemini. Results are typed.",
    "keyPoints": ["query", "structured output"]
  },
  "finishReason": "STOP",
  "modelVersion": "gemini-3.5-flash-lite",
  "usage": {"promptTokens": 40, "candidateTokens": 20, "thoughtsTokens": 0, "cachedContentTokens": 0, "totalTokens": 60}
}
```

Text over 64 KiB, an empty text, or a title over 200 characters fails the
Flow before Gemini is called.

## Test

From `connectors/google/gemini`:

```bash
GOWORK=off go test -race ./examples/generate-summary/...
```

The real Dex suite starts Flows through the Dex Client against a local fake
Gemini API. It covers the generated, blocked, and truncated routes, an unwired
`providerRejected` outcome that fails without a retry, and invalid input:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/generate-summary/... -count=1
```
