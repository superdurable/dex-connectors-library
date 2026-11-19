# VariFlight get flight status Flow

Install with `uv sync --locked --project connectors/variflight/python`. Configure the `variflight` connection in Dex Web Connections using [the provider guide](../../../connector.yaml). From this package directory, run `uv run python -m examples.flight_status.worker` with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev -open=false`. Start `VariFlightStatus` with `{"departureDate": "2026-11-13", "flightNumber": "GX8915"}`. Only `found` is wired; optional outcomes fail the Flow for inspection.

The checked-in integration test uses a fake provider and real Dex Worker/Client. Real provider authorization and API availability remain unverified.
