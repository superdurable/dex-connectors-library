# Dimleap model catalog Flow

This runnable Flow uses the static `dimleap` connection to list model IDs available to its API Key. The `found` branch records count and at most the first 50 IDs in a registered Dex Attribute and completes. Optional `providerRejected`, `invalidResponse`, and `defect` branches are unwired and fail the Flow when selected.

Create the connection as described in the [Python connector README](../../README.md). There are no operation-specific fields. Set `DEX_FLOW_SERVICE_ADDRESS` and the Dex project configuration file. From `connectors/dimleap/python`, run:

```bash
uv sync --locked
uv run python -m examples.model_catalog.worker
```

Start a lookup with a unique Flow ID:

```bash
uv run python -m examples.model_catalog.start dimleap-catalog-001
```

The Flow input is `{}`. Generate strict FDG 2.0:

```bash
dexcli visualize examples/model_catalog/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/dimleap-catalog-python-fdg --python .venv/bin/python
```

The integration test uses an inert Key and synthetic provider response against a real Dex Worker and Client. It checks Bearer authentication, 503 Retry, and safe completion. Live Dimleap authorization remains unverified; the model directory varies by Key and time.
