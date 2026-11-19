# Sensors Data campaign catalog Flow

From `connectors/sensorsdata/python`, run `uv sync --group dev`, configure the Dex Web Connection `sensorsdata` using the [Sensors Data setup guide](https://manual.sensorsdata.cn/sa/docs/open_api_authentication), and start `dexcli dev -open=false`. Run `uv run python -m examples.campaign_catalog.worker` to serve the Flow. Start `CampaignCatalogFlow` with `ListCampaignsInput(page_num=1, page_size=20)`. The `listed` branch completes the Flow; optional outcomes fail it.

Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/campaign_catalog/integration_test.py` to verify the Flow with a fake provider through a real Dex Worker, Client, persistence, and 503 retry.

Generate and validate FDG 2.0 with:

```bash
dexcli visualize examples/campaign_catalog/flow.py --language python --schema-version 2.0 --lint app --json --out - --python .venv/bin/python --open=false
```
