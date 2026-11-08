# Nimble Python contact ID discovery

This runnable Flow calls `listContactIDs` through the static `nimble` connection, stores a bounded contact ID page in an Attribute, and completes with the typed result. In [Nimble CRM](https://app.nimble.com/), open **Settings > API Token > Generate New Token** as an administrator and save the token in Dex secret `api_key`. An administrator may need to grant API access under **Settings > Users**.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from `connectors/nimble/python`:

```bash
uv run python -m examples.contact_id_discovery.worker
```

Start Flow type `NimbleContactIDDiscovery` with a unique Flow ID and input `{"page":1,"perPage":20}`. The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected.

Generate a strict FDG 2.0 graph with `dexcli visualize ./examples/contact_id_discovery/flow.py --language python --python .venv/bin/python --schema-version 2.0 --json --out /tmp/nimble-python-fdg --open=false`. Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8947 uv run pytest -q examples/contact_id_discovery/integration_test.py` against `dexcli dev` for the real Worker retry path. Live Nimble authorization has not been tested.
