# EasyAPI company service Flow

This runnable Python Flow uses the static `easyapi` connection to read service configuration for a company already associated with the key pair. The `found` branch records only `shopId`, `name`, `apiState`, and `endTime` in a Dex Attribute and completes. Optional `providerRejected`, `invalidResponse`, and `defect` branches are unwired and fail the Flow when selected.

Create the connection as described in the [Python connector README](../../README.md). There are no operation-specific configuration fields. Set `DEX_FLOW_SERVICE_ADDRESS` and the Dex project configuration file. From `connectors/easyapi/python`, run:

```bash
uv sync --locked
uv run python -m examples.shop.worker
```

Start a query with a unique Flow ID and an authorized tax number:

```bash
uv run python -m examples.shop.start easyapi-shop-001 91320211MA1WML8X6T
```

Generate strict FDG 2.0:

```bash
dexcli visualize examples/shop/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/easyapi-shop-python-fdg --python .venv/bin/python
```

The integration test uses inert credentials and a fake provider with a real Dex Worker and Client. It checks 503 Retry and exclusion of credentials, tax number, address, phone, bank, and staff fields from output. Live EasyAPI authorization has not been verified.
