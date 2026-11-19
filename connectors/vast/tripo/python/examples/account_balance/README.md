# Tripo get account balance Flow

Install with `uv sync --locked --project connectors/vast/tripo/python`. Configure the `tripo` connection in Dex Web Connections using [the provider guide](../../../connector.yaml). From this package directory, run `uv run python -m examples.account_balance.worker` with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev -open=false`. Start `TripoAccountBalance` with `{}`. Only `fetched` is wired; optional outcomes fail the Flow for inspection.

The checked-in integration test uses a fake provider and real Dex Worker/Client. Real provider authorization and API availability remain unverified.
