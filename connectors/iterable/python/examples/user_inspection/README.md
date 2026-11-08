# Iterable Python user inspection

This runnable Flow calls `getUser` through the static `iterable` connection, persists the selected user identity, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/iterable/python`, run `uv run python -m examples.user_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `IterableUserInspectionPython` with a unique Flow ID and input `{"email":"user@example.com"}`. Replace the sample email with one returned by Iterable.
