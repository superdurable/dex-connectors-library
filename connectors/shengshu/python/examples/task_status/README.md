# Vidu task status Flow

This runnable Python Flow queries Vidu task status through the static `shengshu` connection and persists only the bounded result. Configure the API key in Dex Web Connections using the connector authorization guide, then run `python -m examples.task_status.worker` from this package directory with `DEX_FLOW_SERVICE_ADDRESS` set to the Dex Flow service.

Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run --group dev pytest -q examples/task_status/integration_test.py` against `dexcli dev -open=false` to verify a 503 retry and persisted result.
