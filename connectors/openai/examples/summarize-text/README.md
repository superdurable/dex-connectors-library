# OpenAI summarize-text example

This example runs one operation-only Flow from Dex Web **Start Flow**:

1. `RecordSummaryRequest` validates the typed start input and persists it in
   the `openai-summary-request` Attribute;
2. `SummarizeText` calls `openai.NewCreateResponseStep`, which stores one
   OpenAI Response and streams the summary to the `openai-summary-text`
   Stream as the model writes it;
3. `RecordSummaryOutcome` persists the `completed` or `failed` Response, with
   its ID, status, and usage, in the `openai-summary-outcome` Attribute and
   completes the Flow.

The summary and display RPCs show the request and the outcome in Dex Web.
The example leaves `providerRejected`, `uncertain`, and `defect` unwired, so
those outcomes fail the Flow. For example, a project without credits gets
HTTP 429, which `createResponse` retries until its retry policy ends. An
`uncertain` outcome means OpenAI may have stored the Response; reconcile it
with `retrieveResponse` when the Response ID is known. Generation Steps that
need no stored Response use the
[llm connector](../../../superdurable/llm/README.md) instead.

The Flow and every application Step declare stable types with `GetFlowType`
and `GetStepType`, because Dex Web Start Flow sends the type names from the
Flow Definition. The start Step implements a `WaitFor` that skips
immediately, because Dex Web Start Flow invokes the start Step's `WaitFor`.

## Validate the Flow Definition

From `connectors/openai`, generate strict FDG 2.0 with the latest stable dexcli
release:

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
npm ci --prefix connectors/openai/ui && npm run build --prefix connectors/openai/ui
mkdir -p /tmp/openai-release
go run ./cmd/connectorctl ui-artifact \
  --manifest connectors/openai/connector.yaml \
  --ui-root connectors/openai/ui/dist \
  --output /tmp/openai-release/connector-ui.tgz \
  --digest-output /tmp/openai-release/connector-ui.tgz.sha256
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/openai/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/openai \
  --version v0.21.0 --tag connectors/openai/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/openai-release/connector-ui.tgz \
  --ui-digest /tmp/openai-release/connector-ui.tgz.sha256 \
  --output /tmp/openai-release/connector-release.json \
  --digest-output /tmp/openai-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/openai/build" \
  --connector-release-override openai=/tmp/openai-release
```

Open the Dex Web URL that dexcli prints and select **Connections**. Select
`openai / openai-api`, which the `SummarizeText` Step of
`OpenAISummarizeText` uses. In the host-owned form, enter an OpenAI API key
in `api_key`, leave `model` blank for `gpt-6-sol` or enter another model,
leave `endpoint`, `maxResponseBytes`, and `maxSseEventBytes` at their
defaults, and save. The status becomes **Ready**.

Then open the `createResponse · SummarizeText` tab. Its **Summary model**
picker lists OpenAI's models live, newest first. Pick one, keep the
connection's model, or type a model ID, and select **Save**.

Dex Web saves the settings, the Step pick, and the key in the project
configuration. In a second terminal, start the Worker from `connectors/openai`
with that project's `DEX_PROJECT_*` environment, which
[`sdkgo/projectconfig`](../../../../sdkgo/projectconfig/README.md#application-loading)
documents, including `DEX_PROJECT_ALLOW_LOCAL_STORAGE=true` and
`DEX_PROJECT_STORAGE_ENDPOINT` for a local S3-compatible store:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/openai"
go run ./examples/summarize-text
```

The Worker reads the connection's settings and the Step's pick at startup,
so restart it after changing either in Dex Web; a replaced key applies
without a restart. It listens on `127.0.0.1:8821`; override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed. It logs the connection name, never the key.

In the Run workspace, choose **Start Flow**, select `OpenAISummarizeText`,
choose the Worker at `127.0.0.1:8821`, enter a Flow ID, and submit:

```json
{"text": "The Dex OpenAI connector stores a Response through the Responses API, streams the text, and returns typed branches."}
```

The run completes with a `SummaryOutcome` like this, and the run detail
shows the same value under `openai-summary-outcome`:

```json
{
  "branch": "completed",
  "summary": "The Dex OpenAI connector stores a Response through the Responses API and streams the text.",
  "responseId": "resp_68f0c3a1b2c4d5e6",
  "model": "gpt-6-sol",
  "status": "completed",
  "usage": {"inputTokens": 45, "cachedInputTokens": 0, "outputTokens": 160, "reasoningOutputTokens": 130, "totalTokens": 205}
}
```

Empty text, or text over 64 KiB, fails the Flow before OpenAI is called.

## Test

From `connectors/openai`:

```bash
GOWORK=off go test -race ./examples/summarize-text/...
```

The real Dex test opens the connection from project configuration and
in-memory project storage, as the Worker does, and starts the Flow through
the Dex Client against a local fake Responses API. It covers a completed
Response with its streamed text and the connection's model, and an
incomplete Response for the Step's pick that completes as `failed`, and
checks the idempotency key on every request:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/summarize-text/... -count=1
```
