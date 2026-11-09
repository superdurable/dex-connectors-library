# Facebook Lead Ads lead-form Flow (Python)

This checked-in Dex Flow reads one Page of lead-form IDs, names, and statuses using the static `facebook-lead-ads` connection. `listLeadForms` never requests form questions or lead answers. The `page_access_token` setup and permissions are documented in the [connector README](../../../README.md).

From `connectors/meta/facebook-lead-ads/python`, install with `uv sync --locked`. Generate an FDG 2.0 graph with:

```bash
uv run dexcli visualize examples/lead_forms/flow.py --schema-version 2.0 --lint app --json --out /tmp/facebook-lead-ads-python-fdg --open=false
```

Start `dexcli dev` separately and set `DEX_FLOW_SERVICE_ADDRESS` for the real Dex integration test:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8981 uv run pytest examples/lead_forms/integration_test.py
```

The test supplies a synthetic token and mock Meta transport, then checks a 503 retry and completion across a real Dex Worker and Client. Live Meta authorization has not been verified.
