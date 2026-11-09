# RabbitMQ Python queue inspection

This runnable Flow calls `getQueueMetrics` through the static `rabbitmq` connection, persists queue metrics, and completes. Optional branches are unwired and fail the Flow when selected. Configure `managementUrl`, `username`, and secret `password` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/rabbitmq/python`, run `uv run python -m examples.queue_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `RabbitMQQueueInspectionPython` with a unique Flow ID and input `{"virtualHost":"/","name":"work"}`. Replace the sample with an existing virtual host and queue name from the RabbitMQ Management UI.

Generate and lint the Flow Definition Graph 2.0 from the same directory:

```bash
dexcli visualize examples/queue_inspection/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/rabbitmq-python-fdg --open=false --python .venv/bin/python
```
