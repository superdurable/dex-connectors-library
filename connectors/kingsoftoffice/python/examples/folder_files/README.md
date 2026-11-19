# WPS folder metadata Flow

From `connectors/kingsoftoffice/python`, run `uv sync --group dev`, configure Dex Web Connection `kingsoftoffice` using the [WPS developer portal](https://open.wps.cn), and start `dexcli dev -open=false`. Run `uv run python -m examples.folder_files.worker` to serve the Flow. Start `FolderFilesFlow` with `ListFolderFilesInput("drive-1", "0")` for an authorized drive and folder. The `listed` branch completes the Flow; optional outcomes fail it.

Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/folder_files/integration_test.py` to verify the Flow with a fake provider through a real Dex Worker, Client, persistence, and 503 retry. Live WPS credentials and provider responses remain unverified.

Generate and validate FDG 2.0 with:

```bash
dexcli visualize examples/folder_files/flow.py --language python --schema-version 2.0 --lint app --json --out - --python .venv/bin/python --open=false
```
