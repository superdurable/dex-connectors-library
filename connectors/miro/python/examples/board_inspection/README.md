# Miro Python board inspection

This runnable Flow calls `getBoard` through the static `miro` connection, persists the selected board metadata, and completes. Optional branches are unwired and fail the Flow when selected. Configure `access_token` in Dex Web Connections as described in the [connector README](../../README.md).

From `connectors/miro/python`, run `uv run python -m examples.board_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `MiroBoardInspectionPython` with a unique Flow ID and input `{"boardId":"uXjVOD6LSME="}`. Replace the sample ID with the ID from a real Miro board URL.

Run `uv run pytest -q` for deterministic package tests. With `DEX_FLOW_SERVICE_ADDRESS` set to the local Dex service, run `uv run pytest examples/board_inspection/integration_test.py -q` for the real Worker and Client path.
