# Facebook Pages text publishing Flow (Python)

This checked-in Dex Flow publishes one plain-text Page post using the static `facebook-pages` connection and stores the confirmed post ID. `providerRejected` and `uncertain` remain optional unwired branches; inspect the Page before manually retrying an uncertain publish. See the [connector README](../../../README.md) for Page token setup and the `pages_manage_posts` permission.

From `connectors/meta/facebook-pages/python`, install with `uv sync --locked`. Generate an FDG 2.0 graph with:

```bash
uv run dexcli visualize examples/post_publish/flow.py --schema-version 2.0 --lint app --json --out /tmp/facebook-pages-python-publish-fdg --open=false
```

Start `dexcli dev` separately and set `DEX_FLOW_SERVICE_ADDRESS` for the real Dex integration test:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8981 uv run pytest examples/post_publish/integration_test.py
```

The test supplies a synthetic token and mock Meta transport, then checks a 503 retry, completion, and an uncertain HTTP 500 that fails without resending across a real Dex Worker and Client. Live Meta authorization has not been verified.
