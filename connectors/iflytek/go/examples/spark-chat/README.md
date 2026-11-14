# iFLYTEK Spark Chat Flow

This runnable Flow calls `createChatCompletion` through the static `iflytek` connection, stores the assistant answer, and completes with a typed result. Optional branches fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/iflytek/go`, run:

```bash
dexcli visualize ./examples/spark-chat/flow/workflow.go --schema-version 2.0 --json --out /tmp/iflytek-spark-chat-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override iflytek=<artifact-directory>`. In Dex Web Connections select `iflytek`, find APIPassword at the provider console path in the [connection guide](../../../README.md), and enter it in the secret field. Leave other fields blank for displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/iflytek/go`:

```bash
GOWORK=off go run ./examples/spark-chat
```

Start `IFLYTEKChatCompletion` with a unique Flow ID and input `{ "prompt": "Hello" }`. The Worker connects to Dex Server without an inbound listener. The real-Dex integration test runs with a mock connection. Live provider generation remains unverified.
