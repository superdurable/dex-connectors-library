# ClickUp task event Flow

This runnable Python application receives signed `taskEvent` deliveries at
`/clickup/task-events`, stores each event in the `clickup-task-event` Attribute,
and completes one `ClickUpTaskEventPython` Flow per ClickUp history item. A
redelivery uses the same Flow ID. The `task-events` binding belongs to the
static `clickup-workspace` connection. Its Dex Web event picker can select
specific task events; an empty selection accepts every task event subscribed by
the ClickUp webhook.

From `connectors/clickup/python`:

```sh
uv sync --locked
dexcli visualize examples/task_event/flow.py --language python \
  --schema-version 2.0 --lint app --json --out build/task-event \
  --python .venv/bin/python
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
uv run --with uvicorn python -m examples.task_event.main
```

The server listens on `CLICKUP_BIND_ADDRESS` (default `127.0.0.1:8864`).
`GET /readyz` answers 200 after the binding is ready. Expose the webhook path
through a public HTTPS tunnel, create a ClickUp webhook for the task events
you want, and put the returned `webhook.secret` in the connection's
`webhook_secret` field. The [connector README](../../../README.md#taskevent-trigger)
shows the ClickUp creation request. Without the secret, the endpoint answers
503; bad signatures answer 400. A successful response means the event was
persisted before it was acknowledged, so a process restart can replay it.

`integration_test.py` runs the same Flow, Worker, and endpoint against a real
Dex Server and an inert signed ClickUp delivery. It verifies that redelivery
finds the same Flow:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
uv run pytest examples/task_event/integration_test.py
```

Live ClickUp webhook delivery has not been verified.
