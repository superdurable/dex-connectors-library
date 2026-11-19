# Enlvon policy directory Flow

This runnable Python Flow uses the static `enlvon` connection to search electricity policy cards. Its `found` branch records only title, date, source, and original government URL in a Dex Attribute and completes. Optional `providerRejected`, `invalidResponse`, and `defect` branches are unwired and fail the Flow when selected.

Create the connection as described in the [Python connector README](../../README.md). There are no operation-specific configuration fields. Set `DEX_FLOW_SERVICE_ADDRESS` and the Dex project configuration file. From `connectors/enlvon/python`, run:

```bash
uv sync --locked
uv run python -m examples.policy_directory.worker
```

Start a search with a unique Flow ID, natural-language query, and optional result count:

```bash
uv run python -m examples.policy_directory.start enlvon-search-001 分时电价 --top-k 1
```

Generate strict FDG 2.0 without opening a browser:

```bash
dexcli visualize examples/policy_directory/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/enlvon-policy-python-fdg --python .venv/bin/python --open=false
```

The integration test uses an inert key and fake provider against a real Dex Worker and Client. It checks 429 Retry, source-linked output, and removal of detailed analysis. Live Enlvon authorization has not been verified.
