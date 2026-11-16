# Apifox Python project-summary example

Run `uv run python -m examples.project_summary.worker` from
`connectors/apifox/python` after `dexcli dev` is running and Dex Web
Connections has an `apifox` connection. The Worker reads that connection
through project configuration.

Generate and validate the Flow Definition Graph 2.0 with:

```sh
dexcli visualize examples/project_summary/flow.py --language python --schema-version 2.0 --json --out - --open=false --python .venv/bin/python
```

The checked-in `integration_test.py` starts this Flow against a real Dex
Worker and Client, retries one synthetic provider 503, and reads the persisted
result. Set `DEX_FLOW_SERVICE_ADDRESS` to a running `dexcli dev` server before
running it. No real Apifox account or secret is needed for that test.
