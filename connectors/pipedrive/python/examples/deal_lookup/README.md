# Pipedrive deal lookup (Python)

This runnable Flow reads one complete Pipedrive deal through the static `pipedrive-crm` connection. Create that connection in Dex Web with a Personal API token or Pipedrive OAuth app, following the authorization guide shown there. The Flow starts with `GetObjectInput(object_type="deals", object_id="42")` and returns a `CRMObject` when Pipedrive finds the deal. The optional `notFound`, `providerRejected`, `invalidResponse`, and `defect` branches are intentionally unwired and fail the Flow.

From the repository root, generate and validate the strict FDG 2.0 graph before using Dex Web:

```sh
dexcli visualize connectors/pipedrive/python/examples/deal_lookup/flow.py \
  --language python --python connectors/pipedrive/python/.venv/bin/python \
  --schema-version 2.0 --json --out /tmp/pipedrive-deal-lookup
```

Start `dexcli dev` on free local ports. Set `DEX_FLOW_SERVICE_ADDRESS` to its Dex gRPC address and `DEX_CONNECTOR_CONFIG_FILE` to the owner-only project connection file written by Dex Web. From `connectors/pipedrive/python`, run `uv sync --locked`, then `uv run python -m examples.deal_lookup.worker` in one terminal. Start a Flow with `uv run python -m examples.deal_lookup.start <unique-flow-id> <deal-id>`.

The automated integration test uses a real Dex Worker and Client with a local HTTP stand-in. Run it from `connectors/pipedrive/python` with `DEX_FLOW_SERVICE_ADDRESS` set to a running `dexcli dev` instance: `uv run pytest examples/deal_lookup/integration_test.py`. Live Pipedrive authorization and API behavior remain unverified.
