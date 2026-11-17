# FastGPT knowledge base details Flow

`GetDatasetDetailsFlow` calls `getDatasetDetails` using the static `fastgpt` connection, persists the filtered ID/name/type/status Attribute, and completes. Its optional `providerRejected`, `invalidResponse`, and `defect` branches remain unwired, so those definite outcomes fail the Flow. Temporary provider errors retry under the Step policy.

In Dex Web Connections, create `fastgpt`. Start at [FastGPT OpenAPI](https://doc.fastgpt.cn/zh-CN/openapi/intro), then open **账号 > API 密钥** and create a personal API Key with knowledge base read access. Put it in secret `api_key`. An application chat Key is insufficient. `baseUrl` can be blank for China cloud; set an approved private HTTPS instance including its deployment subpath but no `/api`. Leave `timeout` (Go duration) and `maxResponseBytes` (bytes) blank for their defaults. There are no operation configuration fields. Supply a knowledge base `id` from its FastGPT URL.

From `connectors/fastgpt/python`, run `uv sync --locked`, start `dexcli dev -open=false`, and run `uv run python -m examples.get_dataset_details.worker` with Dex project connection environment. Generate strict FDG 2.0 with:

```sh
uv run dexcli visualize examples/get_dataset_details/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/dex-fastgpt-python-details-fdg --open=false --python .venv/bin/python
```

With `DEX_FLOW_SERVICE_ADDRESS` pointing at Dex, `uv run pytest examples/get_dataset_details/integration_test.py` runs this Flow through a real Worker and Client, retries a synthetic 503, and checks private fields stay out of the output. It uses no real API Key. Live provider authorization remains unverified.
