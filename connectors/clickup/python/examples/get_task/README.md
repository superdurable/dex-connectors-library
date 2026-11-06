# Read a ClickUp task

The Flow uses the static `clickup-workspace` connection and the `getTask` Query. Create that connection in Dex Web using the ClickUp personal API token described in the [connector guide](../../../README.md). From the `python/` directory, run `uv sync --locked`, start `uv run python -m examples.get_task.worker`, then run `uv run python -m examples.get_task.start clickup-task-1 examples/get_task/input.example.json` against the same Dex stack. Set `DEX_FLOW_SERVICE_ADDRESS` when Dex runs on a nondefault port.

The example stores the returned task in a Dex Attribute. An unwired optional branch fails the Flow. The integration test runs the same Flow through a real Dex Worker and Client with an inert local provider stand-in; it does not verify live ClickUp authorization.
