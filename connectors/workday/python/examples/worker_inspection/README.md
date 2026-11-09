# Workday worker inspection (Python)

This runnable Flow calls `new_get_worker_step` with the static `workday` Dex Web connection and persists the returned worker WID and display name. Configure OAuth as described in the [connector guide](../../../README.md), then run `uv run python -m examples.worker_inspection.worker` from the Python package directory.

From the connector directory, generate and lint its FDG 2.0 graph:

```sh
dexcli visualize python/examples/worker_inspection/flow.py --schema-version 2.0 --lint app --json --python python/.venv/bin/python --out /tmp/workday-python-fdg
```

With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `uv run pytest examples/worker_inspection/integration_test.py` from the Python package directory. This exercises the real Dex Worker, Client, retry, and Attribute path with a local provider fixture. Live Workday authorization requires a user-controlled account.
