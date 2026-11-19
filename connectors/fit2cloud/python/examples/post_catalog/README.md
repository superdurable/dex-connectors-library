# Halo post catalog (Python)

Create a connection named `fit2cloud` in Dex Web using the authorization guide in `connector.yaml`. Set `baseUrl` to the HTTPS Halo origin and `api_key` to the secret `pat_` token. The Flow input selects a one-based page and a page size from 1 to 100. Optional outcomes are unwired and fail the Flow when selected.

From `connectors/fit2cloud/python`, run `uv sync --locked`, start `dexcli dev -open=false`, and run `uv run python -m examples.post_catalog.worker` with the Dex project connection environment. Generate strict FDG 2.0 with:

```sh
dexcli visualize examples/post_catalog/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/dex-fit2cloud-python-posts-fdg --open=false --python .venv/bin/python
```

With `DEX_FLOW_SERVICE_ADDRESS` pointing to local Dex, `uv run pytest examples/post_catalog/integration_test.py` starts the Flow through a real Worker and Client, retries a synthetic 503, and checks the persisted page. The test uses no real Halo token; live authorization remains unverified.
