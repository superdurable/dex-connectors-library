# DataEyes read web page

`ReadWebPageFlow` calls `readWebPage` through the static `dataeyes` Dex Web connection, persists the title and Markdown Attribute, and completes with it. The optional `providerRejected`, `invalidResponse`, and `defect` branches are unwired, so selecting one fails the Flow. Reads retry temporary errors under a limited, quota-conscious Step policy.

Create a connection named `dataeyes` in Dex Web. Follow the [search/reading Key guide](https://doc.shuyanai.com/doc-8441886): sign in, open **MCP产品 → 接入与密钥**, and create or copy the secret reading Key. Enter it as `apiKey`; a model Key will not work. Leave `timeout` (Go duration) and `maxResponseBytes` (bytes) blank for their displayed defaults. There are no operation-specific configuration fields. Submit only public HTTPS domain URLs without query strings or fragments.

From `connectors/dataeyes/python`, run `uv sync --locked`, start `dexcli dev`, and run `uv run python -m examples.read_web_page.worker` with the Dex project connection environment. Generate strict FDG 2.0 with:

```sh
uv run dexcli visualize examples/read_web_page/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/dex-dataeyes-python-reader-fdg --open=false --python .venv/bin/python
```

With `DEX_FLOW_SERVICE_ADDRESS` pointing at a running Dex development server, `uv run pytest examples/read_web_page/integration_test.py` starts the Flow through a real Worker and Client, retries a synthetic provider 503, and checks the page output. The test uses no real DataEyes Key. Live provider authorization and page content remain unverified.
