# CTYun knowledge-base directory Flow

This runnable Flow uses the static `ctyun` connection to list accessible knowledge bases. Its `listed` branch records ID, name, and file count in a Dex Attribute and completes. Optional `providerRejected`, `invalidResponse`, and `defect` branches are unwired and fail the Flow when selected.

Create the connection as described in the [Python connector README](../../README.md). There are no operation-specific configuration fields. Set `DEX_FLOW_SERVICE_ADDRESS` and the Dex project configuration file. From `connectors/ctyun/python`, run:

```bash
uv sync --locked
uv run python -m examples.knowledge_base_directory.worker
```

Start a lookup with a unique Flow ID:

```bash
uv run python -m examples.knowledge_base_directory.start ctyun-directory-001
```

The Flow input is `{}`. Generate strict FDG 2.0:

```bash
dexcli visualize examples/knowledge_base_directory/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/ctyun-directory-python-fdg --python .venv/bin/python
```

The integration test uses inert credentials and a fake provider against a real Dex Worker and Client. It checks EOP signing, 503 Retry, and completion. Live CTYun authorization has not been verified.
