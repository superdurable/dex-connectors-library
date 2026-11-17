# APISpace current-weather Flow

Install with `uv sync --locked --project connectors/apispace/python`. Configure
the `apispace` connection in Dex Web Connections with the secret token from the
[provider Access Control page](https://www.apispace.com/eolink/api/456456/apiDocument).
From the Python package directory, run `uv run python -m examples.current_weather.worker`
with `DEX_FLOW_SERVICE_ADDRESS` pointing to
`dexcli dev`. Start `APISpaceCurrentWeather` with `{"areaCode":"101010100"}`
or another area code from the provider's city list. Only `found` is wired;
other outcomes fail the Flow for inspection.

The integration test runs this Flow against real Dex with a fake provider.
Real APISpace authorization, quota, and weather responses remain unverified.
