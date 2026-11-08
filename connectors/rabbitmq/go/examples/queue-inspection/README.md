# RabbitMQ queue inspection Flow

This runnable Flow calls `getQueueMetrics` through the static `rabbitmq` connection, persists the safe counts in an Attribute, and exposes them through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/rabbitmq/go`, run:

```bash
dexcli visualize ./examples/queue-inspection/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/rabbitmq-queue-inspection-fdg --open=false
```

An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override rabbitmq=<artifact-directory>`. In Dex Web Connections select the `rabbitmq` connection. Follow the [connection guide](../../../README.md) to enter `managementUrl`, `username`, and secret `password`; leave `timeout` and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/rabbitmq/go`:

```bash
GOWORK=off go run ./examples/queue-inspection
```

Start `RabbitMQQueueInspection` with a unique Flow ID and input `{"virtualHost":"/","name":"work"}`. Replace the sample with the exact virtual host and queue name from the Management UI. The Worker connects to Dex Server without an inbound listener.

CI runs the fixture-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real RabbitMQ authorization and queue metrics remain unverified.
