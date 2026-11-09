# Sage Intacct invoice inspection (Python)

This runnable Flow calls `new_get_invoice_step` with the static `sage-intacct` Dex Web connection and persists the returned invoice key and ID. Configure OAuth as described in the [connector guide](../../../README.md), then run `uv run python -m examples.invoice_inspection.worker` from the Python package directory.

From the connector directory, generate and lint its FDG 2.0 graph:

```sh
dexcli visualize python/examples/invoice_inspection/flow.py --schema-version 2.0 --lint app --json --python python/.venv/bin/python --out /tmp/sage-intacct-python-fdg
```

With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `uv run pytest examples/invoice_inspection/integration_test.py` from the Python package directory. This exercises the real Dex Worker, Client, retry, and Attribute path with a local provider fixture. Live Sage Intacct authorization requires a user-controlled account.
