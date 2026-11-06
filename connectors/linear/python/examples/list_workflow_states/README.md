# Linear workflow states in a Dex Flow

This runnable Flow lists a team's workflow states through the static `linear`
connection. Create that connection in Dex Web **Connections**, using a personal
API key or Linear OAuth as described in the [connector guide](../../../README.md#linear-setup).
The team UUID is the Flow input. The `listed` branch completes with the states;
other optional branches fail the Flow.

From `connectors/linear/python`, generate strict FDG 2.0 with the installed
Python interpreter:

```sh
dexcli visualize examples/list_workflow_states/flow.py \
  --schema-version 2.0 --json --python .venv/bin/python \
  --out /tmp/linear-python-workflow-states
```

The command reports `valid: true`. Start `dexcli dev` with that flow rendering
directory, configure `linear / linear` in Dex Web, and set
`DEX_CONNECTOR_CONFIG_FILE` to its saved owner-only project file. Then run:

```sh
uv sync --locked
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.list_workflow_states.worker
```

In another terminal, use the checked-in Client entrypoint with a unique Flow ID
and the team UUID from Linear **Settings > Teams > the team > Copy team ID**:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.list_workflow_states.start \
  linear-states-001 2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4
```

The entrypoint calls `start_workflow_state_flow` in
[`start.py`](start.py). The Worker in [`worker.py`](worker.py) loads the current
credential source through `new_project_connection` and registers the Flow in
[`flow.py`](flow.py). To run the real Dex Worker and Client integration test with
an inert provider stand-in, set `DEX_FLOW_SERVICE_ADDRESS` to the local
`dexcli dev` address and run:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  uv run pytest examples/list_workflow_states/integration_test.py
```

Live Linear behavior and OAuth consent were not verified for this Python port.
