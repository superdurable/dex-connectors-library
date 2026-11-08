# Box Python file download Flow

This runnable Flow downloads one file by numeric ID with the static `box` connection. It saves bounded Base64 content in an Attribute. Optional branches are unwired and fail the Flow when selected.

From `connectors/box/python`, generate and check the FDG 2.0 graph:

```bash
dexcli visualize examples/file_download/flow.py --python .venv/bin/python --schema-version 2.0 --lint app --json --out /tmp/box-python-file-download-fdg --open=false
```

After configuring `box / box` in Dex Web as described in the [connector README](../../../README.md), load a Dex project and run `uv run python -m examples.file_download.worker`. Start `BoxDownloadFilePython` with a unique Flow ID and `{"file_id":"12345"}`. The Worker requires only an outbound Dex connection.

For a mock-backed real Dex check, start `dexcli dev` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8981 uv run pytest examples/file_download/integration_test.py`. Live Box authorization remains unverified.
