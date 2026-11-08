# Dropbox Python metadata Flow

This runnable Flow reads one Dropbox path or stable identifier with the static `dropbox` connection. It saves the returned file or folder identity in an Attribute. The optional branches are unwired and fail the Flow when selected.

From `connectors/dropbox/python`, generate and check the FDG 2.0 graph:

```bash
dexcli visualize examples/file_lookup/flow.py --python .venv/bin/python --schema-version 2.0 --lint app --json --out /tmp/dropbox-python-file-lookup-fdg --open=false
```

After configuring `dropbox / dropbox` in Dex Web as described in the [connector README](../../../README.md), load a Dex project and run `uv run python -m examples.file_lookup.worker`. Start `DropboxGetMetadataPython` with a unique Flow ID and `{"path":"/example.txt"}`. The Worker requires only an outbound Dex connection.

For a mock-backed real Dex check, start `dexcli dev` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8981 uv run pytest examples/file_lookup/integration_test.py`. Live Dropbox authorization remains unverified.
