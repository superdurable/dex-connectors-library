# kuaidibird queryTracking example

`QueryTrackingFlow` calls the manifest-declared `queryTracking` Query through the static `kuaidibird` connection, persists the typed result as a Dex Attribute, and completes the Flow. Optional provider failure branches are unwired and therefore fail the Flow. The integration test starts a real Dex Worker and Client using the connector's generated provider mock; `tests/test_conformance.py` separately replays exact Go-recorded HTTP requests and outcomes.

Create a `kuaidibird` connection in Dex Web using the authorization guide in `../../../connector.yaml`. Its configuration fields already carry provider links, formats, blank behavior, and displayed defaults. The Query has no separate configuration UI fields.

From `connectors/kuaidibird/python`, run `uv sync --locked`; start `dexcli dev -open=false`, then run `uv run python -m examples.query_tracking.worker` with the Dex project connection environment. Generate strict FDG 2.0 with:

```sh
dexcli visualize examples/query_tracking/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/dex-kuaidibird-python-fdg --open=false --python .venv/bin/python
```

Set `DEX_FLOW_SERVICE_ADDRESS` to a running Dex development server and run `uv run pytest examples/query_tracking/integration_test.py`. The checked-in test uses no real provider credential. Live provider authorization and calls remain unverified.
