# X Python post inspection

This runnable Flow calls `getPost` through the static `x-twitter` connection, persists the Post ID and text, and completes. Optional branches are unwired and fail the Flow when selected. Configure `access_token` in Dex Web Connections as described in the [connector README](../../README.md).

From `connectors/x/twitter/python`, run `uv run python -m examples.post_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `XPostInspectionPython` with a unique Flow ID and input `{"postId":"1845123456789012345"}`. Replace the sample ID with the ID from a real X post URL.

Run `uv run pytest -q` for deterministic package tests. With `DEX_FLOW_SERVICE_ADDRESS` set to the local Dex service, run `uv run pytest examples/post_inspection/integration_test.py -q` for the real Worker and Client path.
