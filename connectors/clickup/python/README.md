# ClickUp Python connector

The package implements the ClickUp `searchTasks`, `getTask`, `createTask`, `updateTask`, `updateTaskTags`, `addComment`, and `findMemberByEmail` operations and the signed `taskEvent` webhook Trigger for Dex Flows. Install `dex-connector-clickup==0.2.0` alongside a Dex Python application and load its named connection from Dex project configuration. The connection uses a ClickUp personal API token and resolves it before each provider request.

The [getTask runnable example](examples/get_task/README.md) uses a static `clickup-workspace` connection and a real Dex Worker and Client. Its integration test exercises a retry and an unwired rejection branch against a local ClickUp stand-in. Run from this directory with `uv sync --locked`, then `uv run pytest tests/ examples/get_task/integration_test.py` while `dexcli dev` is running and `DEX_FLOW_SERVICE_ADDRESS` points to it.

`createTask` and `addComment` store a Dex heartbeat checkpoint before an unkeyed write. If a retry sees the checkpoint, it reads back instead of sending the write again; an unconfirmable outcome selects `uncertain`. The provider has no idempotency-key support for these calls. The Python package currently has no live ClickUp authorization or API verification.

`getTask` returns custom field JSON values only when Python can represent their numbers exactly. A value that would lose precision selects `invalidResponse` and identifies the field. `updateTaskTags` bounds its sequence of provider requests to 25 seconds; an incomplete sequence retries.

The [taskEvent runnable example](examples/task_event/README.md) mounts an ASGI webhook endpoint, persists each verified delivery before acknowledgment, and starts a real Dex Flow. Its unit tests cover signing, filtering, malformed bodies, size limits, absent secrets, and replay after restart. Its integration test covers a signed delivery and duplicate Flow identity against a real Dex Worker. Live ClickUp webhook delivery remains unverified.
