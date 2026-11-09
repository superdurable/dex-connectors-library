# Copper Python lead status discovery

This runnable Flow calls `listLeadStatuses` using the static `copper` connection, stores lead status IDs and names in an Attribute, and completes with the typed result. In [Copper](https://app.copper.com/), open **Settings > Integrations > API Keys**, generate a key, and save it in `api_key` with the generating user's login email in `user_email`. Leave configuration fields blank for their displayed defaults.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from `connectors/copper/python`:

```bash
uv run python -m examples.lead_status_discovery.worker
```

Start Flow type `CopperLeadStatusDiscovery` with a unique Flow ID and input `{}`. The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected.

Generate a strict FDG 2.0 graph with `dexcli visualize ./examples/lead_status_discovery/flow.py --language python --python .venv/bin/python --schema-version 2.0 --json --out /tmp/copper-python-fdg --open=false`. The graph must report `valid: true`; a first local build may show `connector_release_required` until the connector is published. Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8905 uv run pytest -q examples/lead_status_discovery/integration_test.py` against `dexcli dev` for the real Worker retry path. Live Copper authorization has not been tested.
