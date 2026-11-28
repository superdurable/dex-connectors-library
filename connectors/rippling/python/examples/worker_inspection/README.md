# Rippling Python worker inspection

This runnable Flow calls `getWorker` through the static `rippling` connection, persists the selected worker status, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_token` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/rippling/python`, run `uv run python -m examples.worker_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `RipplingWorkerInspectionPython` with a unique Flow ID and input `{"workerId":"65699281fb7bc7c3510f44a6"}`. Replace the sample worker ID with one returned by Rippling.

The same Worker serves [the worker page Flow](list_flow.py). Start `RipplingWorkerListPython` with `{"limit":2}`. Pass the returned `nextCursor` as `cursor` to read the next page. An empty cursor in the result marks the last page.

From the Python package directory, validate the page Flow with `dexcli visualize examples/worker_inspection/list_flow.py --schema-version 2.0 --lint app --json --open=false --python .venv/bin/python --out /tmp/rippling-python-page`.
