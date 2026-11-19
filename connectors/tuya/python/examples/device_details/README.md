# Tuya device metadata Flow

From `connectors/tuya/python`, run `uv sync --group dev`, configure Dex Web Connection `tuya` using the [Tuya developer portal](https://platform.tuya.com/), and start `dexcli dev -open=false`. Run `uv run python -m examples.device_details.worker` to serve the Flow. Start `DeviceDetailsFlow` with `GetDeviceDetailsInput("device123")` for an authorized linked device. The `found` branch completes the Flow; optional outcomes fail it.

Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/device_details/integration_test.py` to verify the Flow with a fake provider through a real Dex Worker, Client, persistence, and 503 retry. The test confirms device local keys and status values are absent from durable results. Live Tuya credentials and provider responses remain unverified.

Generate and validate FDG 2.0 with:

```bash
dexcli visualize examples/device_details/flow.py --language python --schema-version 2.0 --lint app --json --out - --python .venv/bin/python --open=false
```
