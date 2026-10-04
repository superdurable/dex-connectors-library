# LLM summarize-text example

This example runs one operation-only Flow from Dex Web **Start Flow**:

1. `RecordSummaryRequest` validates the typed start input and persists it in
   the `llm-summary-request` Attribute;
2. `SummarizeText` calls `llm.NewGenerateTextStep` with the Step's model
   pick, or the connection default without one, and the connection's
   provider writes the summary to the `llm-summary-text` Stream;
3. `RecordSummaryOutcome` persists the `generated`, `truncated`, or `blocked`
   outcome, with the provider and model, in the `llm-summary-outcome`
   Attribute and completes the Flow;
4. `RecordSummaryFailure` persists a `providerRejected`, `invalidResponse`,
   or `defect` outcome, with the provider and the safe failure message, in
   the `llm-summary-failure` Attribute and fails the Flow with that message.

The summary and display RPCs show the request, the outcome, and any failure
in Dex Web. The start input takes no model, so a person who starts the Flow
cannot choose a model to bill; the connector README shows the opt-in variant.
The request sets no temperature, effort, or output limit, so every model the
picker lists accepts it.

The Flow and every application Step declare stable types with `GetFlowType`
and `GetStepType`, because Dex Web Start Flow sends the type names from the
Flow Definition. The start Step implements a `WaitFor` that skips
immediately, because Dex Web Start Flow invokes the start Step's `WaitFor`.

## Validate the Flow Definition

From `connectors/superdurable/llm`, generate strict FDG 2.0 with the latest
stable dexcli release:

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
  --version v0.21.0 --tag connectors/superdurable/llm/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/llm-release/connector-ui.tgz \
  --ui-digest /tmp/llm-release/connector-ui.tgz.sha256 \
  --output /tmp/llm-release/connector-release.json \
  --digest-output /tmp/llm-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/superdurable/llm/build" \
  --connector-release-override llm=/tmp/llm-release
```

Open the Dex Web URL that dexcli prints and select **Connectors**. Select
**LLM** (connection `llm`), which the `SummarizeText` Step of
`LLMSummarizeText` uses. In the host-owned form:

- choose the `provider`, such as `anthropic`, and enter its key in
  `api_key`, following the guide's link to that provider's key page;
- set `region` only for a Qwen or Kimi key created in a China region, or for
  Mistral or xAI regional inference, and `anthropicWorkspaceId` only for a
  Claude key that spans several workspaces;
- leave `maxResponseBytes` blank, and save.

After the save, the connection's `model` field shows the model picker with
the provider's live models. Pick the connection's default model and select
**Save**, or keep **Connector default (the provider's default model)**.

Then open the `generateText · SummarizeText` tab. Its **Summary model** picker
lists the same provider's models. Pick one, keep **Connection default
(...)**, which names the connection's model, or type a model ID of the
provider, and select **Save**.

Dex Web saves the settings, the Step pick, and the key in the project
configuration. In a second terminal, start the Worker from
`connectors/superdurable/llm` with that project's `DEX_PROJECT_*`
environment, which
[`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md#application-loading)
documents, including `DEX_PROJECT_ALLOW_LOCAL_STORAGE=true` and
`DEX_PROJECT_STORAGE_ENDPOINT` for a local S3-compatible store:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/superdurable/llm"
go run ./examples/summarize-text
```

The Worker reads the connection's settings and the Step's pick at startup,
so restart it after changing either in Dex Web; a replaced key applies
without a restart. It listens on `127.0.0.1:8839`; override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed. It logs the connection name and the pick, never a key.

In the Run workspace, choose **Start Flow**, select `LLMSummarizeText`,
choose the Worker at `127.0.0.1:8839`, enter a Flow ID, and submit:

```json
{"text": "The Dex LLM connector runs one generateText Step on the model provider that the connection names and returns typed branches."}
```

With `anthropic` as the provider and no model picked, the run completes with
a `SummaryOutcome` like this, and the run detail shows the same value under
`llm-summary-outcome`:

```json
{
  "branch": "generated",
  "summary": "The Dex LLM connector runs one generateText Step on the connection's model provider.",
  "provider": "anthropic",
  "model": "claude-sonnet-5",
  "servedModel": "claude-sonnet-5",
  "finishReason": "stop",
  "usage": {"inputTokens": 40, "outputTokens": 90, "totalTokens": 130}
}
```

A pick that is not a valid model ID, such as one with a space, fails the run
through `RecordSummaryFailure` with a `defect` message before the provider is
called; a model the provider does not serve fails it with
`providerRejected`. Empty text, or text over 64 KiB, fails the Flow before
any provider is called.

## Test

From `connectors/superdurable/llm`:

```bash
GOWORK=off go test -race ./examples/summarize-text/...
```

The real Dex test opens each connection from project configuration and
in-memory project storage, as the Worker does, and starts the Flow through
the Dex Client against local fake DeepSeek, Mistral, Claude, and Kimi APIs. It
covers the provider's default model streaming its text, a connection model, a
Claude pick that is blocked, a rejected key that records the failure, and a
pick the provider's model-ID rule rejects, which records a defect without a
request:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/summarize-text/... -count=1
```
