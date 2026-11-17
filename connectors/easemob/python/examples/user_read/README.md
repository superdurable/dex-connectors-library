# Easemob user detail Flow

This runnable Python Flow uses the static `easemob` connection to read one IM user's selected account fields. Its `found` branch records user ID, UUID, Unix-millisecond timestamps, active state, and nickname in a Dex Attribute and completes. Optional `providerRejected`, `invalidResponse`, and `defect` branches are unwired and fail the Flow when selected.

Create the connection as described in the [Python connector README](../../README.md). There are no operation-specific configuration fields. Set `DEX_FLOW_SERVICE_ADDRESS` and the Dex project configuration file. From `connectors/easemob/python`, run:

```bash
uv sync --locked
uv run python -m examples.user_read.worker
```

Start a read with a unique Flow ID and the registered lowercase user ID:

```bash
uv run python -m examples.user_read.start easemob-user-001 user1
```

Generate strict FDG 2.0:

```bash
dexcli visualize examples/user_read/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/easemob-user-python-fdg --python .venv/bin/python
```

The integration test uses inert credentials and a fake provider against a real Dex Worker and Client. It checks 503 Retry, 401 token refresh, token reuse across Flows, private push token removal, and completion. Live Easemob authorization has not been verified.
