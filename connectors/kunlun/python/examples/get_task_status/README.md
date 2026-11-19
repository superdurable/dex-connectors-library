# kunlun getTaskStatus example

`GetTaskStatusFlow` calls the manifest-declared `getTaskStatus` Query through the static `kunlun` connection, persists the typed result as a Dex Attribute, and completes the Flow. Optional provider failure branches are unwired and therefore fail the Flow. The integration test starts a real Dex Worker and Client using the connector's generated provider mock; `tests/test_conformance.py` separately replays exact Go-recorded HTTP requests and outcomes.

Create a `kunlun` connection in Dex Web using the authorization guide in `../../../connector.yaml`. Its configuration fields already carry provider links, formats, blank behavior, and displayed defaults. The Query has no separate configuration UI fields.

From `connectors/kunlun/python`, run `uv sync --locked`; start `dexcli dev -open=false`, then run `uv run python -m examples.get_task_status.worker` with the Dex project connection environment. Generate strict FDG 2.0 with:

```sh
dexcli visualize examples/get_task_status/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/dex-kunlun-python-fdg --open=false --python .venv/bin/python
```

Set `DEX_FLOW_SERVICE_ADDRESS` to a running Dex development server and run `uv run pytest examples/get_task_status/integration_test.py`. The checked-in test uses no real provider credential. Live provider authorization and calls remain unverified.
