# Policy lookup example

This Flow lists up to two policy records by `Policy Key` and returns the one
matching record. Zero or multiple matches return `isAmbiguous: true`.

With Dex CLI 1.33.0 or later, generate strict FDG 2.0 from
`connectors/airtable/python` after `uv sync --locked`:

```sh
mkdir -p build
dexcli visualize ./examples/policy_lookup/flow.py --language python \
  --python .venv/bin/python --schema-version 2.0 --json \
  --out ./build/airtable-policy-lookup
```

Start `dexcli dev -flow-rendering-dir ./build`, then configure the static
`airtable / airtable-refund-policies` connection in Dex Web with a personal
access token from <https://airtable.com/create/tokens>. Grant
`data.records:read` and access to the chosen base. Export the owner-only
`DEX_CONNECTOR_CONFIG_FILE` path and `DEX_FLOW_SERVICE_ADDRESS` from that
project, then run the Worker:

```sh
uv run python -m examples.policy_lookup.worker
```

In another terminal with the same environment, start a Flow with a unique ID
and input containing a base ID, table ID or name, and policy key:

```sh
uv run python -m examples.policy_lookup.start airtable-policy-001 examples/policy_lookup/sample-input.json
```

The real Dex integration test uses a local provider fake so no token is needed:
`uv run pytest examples/policy_lookup/integration_test.py` while `dexcli dev` is
running and `DEX_FLOW_SERVICE_ADDRESS` points to it.
