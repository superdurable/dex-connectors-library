# JD Logistics waybill trace Flow

From `connectors/jdlogistics/python`, run `uv sync --group dev`, configure Dex Web Connection `jdlogistics` using the [JD Logistics setup guide](https://open.jdl.com), and start `dexcli dev -open=false`. Run `uv run python -m examples.waybill_trace.worker` to serve the Flow. Start `WaybillTraceFlow` with `QueryWaybillTraceInput("JDX009866825381")` for a waybill that belongs to the authorized merchant. The `found` branch completes the Flow; optional outcomes fail it.

Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/waybill_trace/integration_test.py` to verify the Flow with a fake provider through a real Dex Worker, Client, persistence, and 503 retry. The test also checks that personal details are omitted.

Generate and validate FDG 2.0 with:

```bash
dexcli visualize examples/waybill_trace/flow.py --language python --schema-version 2.0 --lint app --json --out - --python .venv/bin/python --open=false
```
