# Microsoft Advertising campaign discovery (Python)

This runnable Flow calls `new_list_campaigns_step` using the static `microsoft-advertising` Dex Web connection and persists the returned campaign list. Configure OAuth and the developer token as described in the [connector guide](../../../README.md), then run `uv run python -m examples.campaign_discovery.worker` from the Python package directory.

From the connector directory, generate and lint its FDG 2.0 graph:

```sh
dexcli visualize python/examples/campaign_discovery/flow.py --schema-version 2.0 --lint app --json --python python/.venv/bin/python --out /tmp/microsoft-advertising-python-fdg
```

With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `uv run pytest examples/campaign_discovery/integration_test.py` from the Python package directory. The test exercises Worker, Client, retry, and Attribute persistence with a local provider fixture.
