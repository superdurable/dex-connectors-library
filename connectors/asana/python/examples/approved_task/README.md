# Approved Asana task example

This Flow reads one project task page, creates a task if that page has no exact name match,
moves the task to a section, adds one approval comment, and reads back the task. It binds
the static Dex connection name `asana-tasks`. Every optional connector branch is unwired,
so a provider rejection or uncertain write fails the Flow for explicit reconciliation.

Generate a strict Flow Definition Graph 2.0 with Dex CLI 1.28.1 or newer
from `connectors/asana`:

```bash
mkdir -p python/build
dexcli visualize ./python/examples/approved_task/flow.py \
  --schema-version 2.0 --json --out ./python/build/approved_task
```

1. In Dex Web Connections, configure an Asana personal access token for `asana-tasks`.
2. Start a local Dex server, such as `dexcli dev -dex-port 22240 -web-port 22241 -open=false -flow-rendering-dir python/build`.
3. From `connectors/asana/python`, run `uv sync --locked`.
4. Run `DEX_FLOW_SERVICE_ADDRESS=localhost:22240 uv run python -m examples.approved_task.worker`.
5. In another shell, run `DEX_FLOW_SERVICE_ADDRESS=localhost:22240 uv run python -m examples.approved_task.start <unique-flow-id> examples/approved_task/input.example.json` after replacing the sample project and section IDs with yours.

The checked-in integration test uses a fake Asana provider behind a real Dex Worker and
Client: `DEX_FLOW_SERVICE_ADDRESS=localhost:22240 uv run pytest integration/test_approved_task.py`.
Live Asana authorization and provider behavior require a user-controlled connection.
