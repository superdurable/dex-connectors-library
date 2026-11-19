# GSH API model-list Flow

Install with `uv sync --locked --project connectors/gshapi/python`. Configure the `gshapi` connection in Dex Web Connections with the API Key from the [provider console](https://www.gshapi.studio/console). From this Python package directory, run `uv run python -m examples.model_list.worker` with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev`. Start `GSHAPIModelList` with `{}`. Only `found` is wired; optional outcomes fail the Flow for inspection.

The checked-in integration test uses a fake provider and real Dex Worker/Client. Real GSH API authorization and model availability remain unverified.
