# Chukou1 warehouse list

`WarehouseListFlow` calls `listWarehouses` through the static `chukou1` Dex Web connection, persists the bounded `WarehousePage` Attribute, and completes with it. The example's optional `providerRejected`, `invalidResponse`, and `defect` branches are unwired, so selecting one fails the Flow. The Query retries 408, 429, 5xx, and transport failures under the Dex Step retry policy.

Create a connection named `chukou1` in Dex Web. For a self-developed integration, get a fixed token at [Chukou1 Account > Customer Authorization Management](https://yewu.chukou1.cn/Client/Index.aspx) by choosing **Authorize > View Token**, then enter it in the secret `access_token` field without `Bearer`. The default `baseUrl` is the production HTTPS endpoint; `timeout` is measured as a Go duration; `maxResponseBytes` is a byte limit. Leave all three blank for their displayed defaults. There are no operation fields to configure.

From `connectors/chukou1/python`, run `uv sync --locked`, start `dexcli dev`, and run `uv run python -m examples.warehouse_list.worker` with the Dex project connection environment. Generate strict FDG 2.0 with:

```sh
uv run dexcli visualize examples/warehouse_list/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/dex-chukou1-python-warehouses-fdg --open=false --python .venv/bin/python
```

With `DEX_FLOW_SERVICE_ADDRESS` pointing at a running Dex development server, `uv run pytest examples/warehouse_list/integration_test.py` starts this Flow through a real Worker and Client, retries a synthetic provider 503, and checks the persisted warehouse result. The test uses no real Chukou1 token. Live provider authorization and warehouse data remain unverified.
