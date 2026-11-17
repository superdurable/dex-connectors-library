# Dada order status Flow

This runnable Python Flow uses the static `dada` connection to read a third-party delivery order's status. Its `found` branch records order ID, status code, and status label in a Dex Attribute and completes. Optional `providerRejected`, `invalidResponse`, and `defect` branches are unwired and fail the Flow when selected.

Create the connection as described in the [Python connector README](../../README.md). There are no operation-specific configuration fields. Set `DEX_FLOW_SERVICE_ADDRESS` and the Dex project configuration file. From `connectors/dada/python`, run:

```bash
uv sync --locked
uv run python -m examples.order_status.worker
```

Start a lookup with a unique Flow ID and the third-party order ID:

```bash
uv run python -m examples.order_status.start dada-status-001 orderNo1653708880866
```

Generate strict FDG 2.0:

```bash
dexcli visualize examples/order_status/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/dada-status-python-fdg --python .venv/bin/python
```

The integration test uses inert credentials and a fake provider against a real Dex Worker and Client. It checks the signed POST, 503 Retry, removal of rider personal data, and completion. Live merchant authorization has not been verified.
