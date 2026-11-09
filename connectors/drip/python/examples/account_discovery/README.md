# Drip Python account discovery

This runnable Flow calls `listAccounts` using the static `drip` connection, stores the account list in an Attribute, and completes with the typed result. Open [Drip user profile](https://www.getdrip.com/user/edit) and save the personal API Token in secret `api_key`. Leave `baseUrl`, timeout, and response-size fields blank for their displayed defaults.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from `connectors/drip/python`:

```bash
uv run python -m examples.account_discovery.worker
```

Start Flow type `DripAccountDiscovery` with a unique Flow ID and input `{}`. The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected.

Generate a strict FDG 2.0 graph from this package with `dexcli visualize ./examples/account_discovery/flow.py --language python --python .venv/bin/python --schema-version 2.0 --json --out /tmp/drip-python-fdg --open=false`. The graph must report `valid: true` with no diagnostics. Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8905 uv run pytest -q examples/account_discovery/integration_test.py` against `dexcli dev` for the real Worker retry path. Live Drip authorization has not been tested.
