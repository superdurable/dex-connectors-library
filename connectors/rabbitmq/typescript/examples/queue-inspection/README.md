# RabbitMQ TypeScript queue inspection

This runnable Flow calls `getQueueMetrics` through the static `rabbitmq` connection and persists queue metrics. Optional branches are unwired and fail the Flow when selected. Configure `managementUrl`, `username`, and secret `password` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/rabbitmq/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- / work`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Replace the sample with an existing virtual host and queue name from the RabbitMQ Management UI.

Generate and lint the Flow Definition Graph 2.0 from the same directory. The local package link lets Dex CLI resolve the manifest before this package is published:

```bash
ln -s ../.. node_modules/@superdurable/connector-rabbitmq
dexcli visualize examples/queue-inspection/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/rabbitmq-typescript-fdg --open=false
```
