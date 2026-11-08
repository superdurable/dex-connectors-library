# Anthropic model inspection Flow

The Worker in `main.go` registers a single `AnthropicModelInspection` Flow. It calls `getModel` using the static `anthropic` connection, persists the returned model metadata, and exposes it in Dex Web Summary and Display. The Query's optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/anthropic/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/model-inspection/flow/workflow.go --schema-version 2.0 --json --out ./build/model-inspection
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata is available.

## Run with Dex Web

Create the connector release artifact from this source, then pass it to `dexcli dev` with `--connector-release-override anthropic=<artifact-directory>` and the generated Flow rendering. In Dex Web Connections, select `anthropic / anthropic`, create the key at [Claude Platform Settings > API Keys](https://platform.claude.com/settings/keys), and save it as the secret `api_key`. Leave the base URL and safety limits blank to use their displayed defaults.

With the project's `DEX_PROJECT_*` environment configured, run from `connectors/anthropic/go`:

```bash
go run ./examples/model-inspection
```

In Dex Web Start Flow, choose `AnthropicModelInspection`, enter a unique Flow ID, and provide:

```json
{"modelId":"claude-sonnet-4-6"}
```

The result is a `ModelInfo` object. The mock-backed real-Dex integration test runs under the `integration` build tag. This example does not require an inbound listener.
