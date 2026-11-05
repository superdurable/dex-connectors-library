# Amazon SQS Python queue consumer

This Flow receives one batch from the static `amazon-sqs / amazon-sqs-orders`
connection, then deletes each received message. An empty long poll completes
without a deletion. Other SQS rejection branches remain unwired and fail the
Flow, leaving messages for the queue's visibility timeout and redrive policy.

From `connectors/amazon/sqs/python`, install the package and generate strict
Flow Definition Graph 2.0 JSON:

```sh
uv sync --locked
mkdir -p build
dexcli visualize ./examples/queue_consumer/flow.py --language python \
  --python .venv/bin/python --schema-version 2.0 --json \
  --out ./build/amazon-sqs-queue-consumer
```

Start `dexcli dev -flow-rendering-dir ./build`. In Dex Web **Connections**,
configure `amazon-sqs / amazon-sqs-orders` using the
[authorization guide](../../../README.md). Set `defaultQueueUrl` to the full
queue URL, or provide `queueUrl` in the Flow input. The operation Steps have
no Configuration UI fields. Run the Worker with the project's
`DEX_CONNECTOR_CONFIG_FILE` and `DEX_FLOW_SERVICE_ADDRESS` environment:

```sh
uv run python -m examples.queue_consumer.worker
```

Start `SQSQueueConsumer` in Dex Web with a unique Flow ID and input `{}`.
The Flow returns the received batch. Messages must have a valid body MD5 and
receive count before they are deleted.

The real Dex integration test uses a local SQS stand-in with an OS-assigned
port and inert keys; it does not contact AWS:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:<dex-port> \
  uv run pytest examples/queue_consumer/integration_test.py
```
