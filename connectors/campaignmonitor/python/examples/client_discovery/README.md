# Campaign Monitor Python client discovery

This runnable Flow calls `listClients` using the static `campaignmonitor` connection, stores the client list in an Attribute, and completes with the typed result. Open [Campaign Monitor API keys](https://help.campaignmonitor.com/articles/Knowledge/api-keys) through Profile image > Account settings > API keys and save the full key in secret `api_key`. Leave `baseUrl`, timeout, and response-size fields blank for their displayed defaults.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from `connectors/campaignmonitor/python`:

```bash
uv run python -m examples.client_discovery.worker
```

Start Flow type `CampaignMonitorClientDiscovery` with a unique Flow ID and input `{}`. The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected.

Generate a strict FDG 2.0 graph from this package with `dexcli visualize ./examples/client_discovery/flow.py --language python --python .venv/bin/python --schema-version 2.0 --json --out /tmp/campaignmonitor-python-fdg --open=false`. The graph must report `valid: true` with no diagnostics. Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8905 uv run pytest -q examples/client_discovery/integration_test.py` against `dexcli dev` for the real Worker retry path. Live Campaign Monitor authorization has not been tested.
