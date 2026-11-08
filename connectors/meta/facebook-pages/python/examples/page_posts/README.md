# Facebook Pages post feed Flow (Python)

This checked-in Dex Flow reads one Page feed page using the static `facebook-pages` connection. It stores post IDs, messages, timestamps, permalinks, and a cursor. Comments and reactions are excluded. See the [connector README](../../../README.md) for Page token setup.

From `connectors/meta/facebook-pages/python`, install with `uv sync --locked`. Generate an FDG 2.0 graph with:

```bash
uv run dexcli visualize examples/page_posts/flow.py --schema-version 2.0 --lint app --json --out /tmp/facebook-pages-python-fdg --open=false
```

Start `dexcli dev` separately and set `DEX_FLOW_SERVICE_ADDRESS` for the real Dex integration test:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8981 uv run pytest examples/page_posts/integration_test.py
```

The test supplies a synthetic token and mock Meta transport, then checks a 503 retry and completion across a real Dex Worker and Client. Live Meta authorization has not been verified.
