# Zendesk Sell Python lead discovery

This runnable Flow calls `listLeads` through the static `zendesk-sell` connection, stores one bounded lead page in a Dex Attribute, and completes with the typed result. Create a personal access token in [Zendesk Sell](https://app.futuresimple.com/) under Integrations > OAuth > Access Tokens > Add User Token, then save it in Dex `access_token`.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from `connectors/zendesk/sell/python`:

```bash
uv run python -m examples.lead_discovery.worker
```

Start Flow type `ZendeskSellLeadDiscovery` with a unique Flow ID and input `{"page":1,"perPage":20}`. The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected.

Generate a strict FDG 2.0 graph with `dexcli visualize ./examples/lead_discovery/flow.py --language python --python .venv/bin/python --schema-version 2.0 --lint app --json --out /tmp/zendesk-sell-python-fdg --open=false`. Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8955 uv run pytest -q examples/lead_discovery/integration_test.py` against `dexcli dev` for the real Worker retry path. Live provider authorization has not been tested.
