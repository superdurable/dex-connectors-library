# FluRouter model catalog (Python)

Create a connection named `flurouter` in Dex Web using the authorization guide in `connector.yaml`. Enter the secret `sk-il-` API Key and leave timeout and response-size overrides blank for the displayed defaults. The Flow input is empty. Optional outcomes are unwired and fail the Flow when selected.

From `connectors/flurouter/python`, run `uv sync --locked`, start `dexcli dev -open=false`, and run `uv run python -m examples.model_catalog.worker` with the Dex project connection environment. Generate strict FDG 2.0 with:

```sh
dexcli visualize examples/model_catalog/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/dex-flurouter-python-models-fdg --open=false --python .venv/bin/python
```

With `DEX_FLOW_SERVICE_ADDRESS` pointing to local Dex, `uv run pytest examples/model_catalog/integration_test.py` starts the Flow through a real Worker and Client, retries a synthetic 503, and checks the persisted model IDs. The test uses no real FluRouter Key; live authorization remains unverified.
