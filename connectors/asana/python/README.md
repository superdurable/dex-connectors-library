# Asana Python connector

`dex-connector-asana` v0.1.0 provides typed Dex Steps for `listTasks`,
`getTask`, `createTask`, `updateTask`, and `addComment`. It declares no Triggers.
Install the released package with `uv add dex-connector-asana==0.1.0`.

The package's generated `Config`, `Credentials`, operation models, Step
factories, connection constructors, and mock answers follow
[`connector.yaml`](../connector.yaml). Application provider calls run inside
the operation-specific Step Execute handlers. Asana has no idempotency key:
`createTask` and `addComment` use Dex heartbeat checkpoints to prevent a
second POST after an ambiguous outcome, then read back a known object ID.

Use the [approved-task example](examples/approved_task/README.md) to run a
Worker with the static `asana-tasks` connection. It exercises all five
operations through a real Dex Worker and Client in its integration test.
The [connector README](../README.md) covers token setup, branches, and the
provider behavior that remains unverified live.

From this directory, run `uv sync --locked`, `uv run pytest tests`, and, with
`dexcli dev` running, `DEX_FLOW_SERVICE_ADDRESS=localhost:22240 uv run pytest
integration/test_approved_task.py`.
