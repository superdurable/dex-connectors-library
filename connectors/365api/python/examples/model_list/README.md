# 365 API model-list Flow

Install with `uv sync --locked --project connectors/365api/python`. Configure the `hby365` connection in Dex Web Connections with the API Key from the [provider console](https://www.hby365.studio/console). From this Python package directory, run `uv run python -m examples.model_list.worker` with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev`. Start `HBY365ModelList` with `{}`. Only `found` is wired; optional outcomes fail the Flow for inspection.

The checked-in integration test uses a fake provider and real Dex Worker/Client. Real 365 API authorization and model availability remain unverified.
