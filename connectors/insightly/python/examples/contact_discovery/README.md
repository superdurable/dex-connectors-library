# Insightly Python contact discovery

This runnable Flow calls `listContacts` through the static `insightly` connection, stores a bounded contact page in an Attribute, and completes with the typed result. In [Insightly CRM](https://login.insightly.com/), open the top-right user icon > **User Settings > User Settings > API** and copy your personal API key into Dex secret `api_key`. Copy your v3.1 API URL into `baseUrl` if your pod differs from the displayed na1 default.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from `connectors/insightly/python`:

```bash
uv run python -m examples.contact_discovery.worker
```

Start Flow type `InsightlyContactDiscovery` with a unique Flow ID and input `{"top":20,"skip":0}`. The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected.

Generate a strict FDG 2.0 graph with `dexcli visualize ./examples/contact_discovery/flow.py --language python --python .venv/bin/python --schema-version 2.0 --json --out /tmp/insightly-python-fdg --open=false`. Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8941 uv run pytest -q examples/contact_discovery/integration_test.py` against `dexcli dev` for the real Worker retry path. Live Insightly authorization has not been tested.
