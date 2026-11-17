# Caiyun current-weather Flow

The [Flow](flow.py) queries Caiyun v2.6 realtime weather at one coordinate and
persists its bounded metric output. Its static Dex Web Connections name is
`caiyun`; optional provider rejection and invalid-response branches fail the
Flow.

Configure App Key and App Secret using the [connector instructions](../../README.md).
Start `dexcli dev`, generate strict FDG 2.0 with
`dexcli visualize examples/current_weather/flow.py --python .venv/bin/python --schema-version 2.0 --json
--out build/current-weather`, then run
`python -m examples.current_weather.worker` from `connectors/caiyun/python`.
Start `CaiyunCurrentWeatherPython` with
`{"longitude":116.3176,"latitude":39.976}` for the Beijing example. Set
`DEX_FLOW_SERVICE_ADDRESS` for a nondefault Dex server.

`integration_test.py` uses an inert signing pair and local provider stand-in
to verify a real Dex Worker, Client, persistence, and a 503 retry. Live Caiyun
credentials and weather data remain unverified.
