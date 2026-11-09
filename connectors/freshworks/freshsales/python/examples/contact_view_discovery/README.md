# Freshsales Python contact discovery

This runnable Flow calls `listContactViews` through the static `freshsales` connection, stores contact views in an Attribute, and completes with the typed result. In [Freshsales CRM](https://app.freshworks.com/), open the profile picture > Profile Settings > API Settings and copy your personal API key into Dex secret `api_key`. Copy the Bundle alias URL ending in `/crm/sales/api` into `baseUrl`.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from `connectors/freshworks/freshsales/python`:

```bash
uv run python -m examples.contact_view_discovery.worker
```

Start Flow type `FreshsalesContactViewDiscovery` with a unique Flow ID and input `{}`. The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected.

Generate a strict FDG 2.0 graph with `dexcli visualize ./examples/contact_view_discovery/flow.py --language python --python .venv/bin/python --schema-version 2.0 --json --out /tmp/freshsales-python-fdg --open=false`. Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8941 uv run pytest -q examples/contact_view_discovery/integration_test.py` against `dexcli dev` for the real Worker retry path. Live Freshsales authorization has not been tested.
