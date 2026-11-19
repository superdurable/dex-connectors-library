# Wuliang AI account balance

`AccountBalanceFlow` calls `getAccountBalance` through the static `dieyuyun` Dex Web connection, persists the exact CNY balance Attribute, and completes with it. The optional `providerRejected`, `invalidResponse`, and `defect` branches are unwired, so selecting one fails the Flow. Temporary failures retry under the Dex Step policy.

Create a connection named `dieyuyun` in Dex Web. At the [Wuliang AI console](https://open-console.dieyuyun.com/), open **API Keys > Key Management**, click **Create API Key**, select an application, and copy the one-time-shown secret `sk-` Key to `api_key`. Leave `baseUrl` (HTTPS origin), `timeout` (Go duration), and `maxResponseBytes` (bytes) blank for their displayed defaults. There are no operation configuration fields or input fields.

From `connectors/dieyuyun/python`, run `uv sync --locked`, start `dexcli dev`, and run `uv run python -m examples.account_balance.worker` with the Dex project connection environment. Generate strict FDG 2.0 with:

```sh
uv run dexcli visualize examples/account_balance/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/dex-dieyuyun-python-balance-fdg --open=false --python .venv/bin/python
```

With `DEX_FLOW_SERVICE_ADDRESS` pointing at a running Dex development server, `uv run pytest examples/account_balance/integration_test.py` starts the Flow through a real Worker and Client, retries a synthetic provider 503, and checks the exact decimal balance. It uses no real API Key. Live authorization and account balance remain unverified.
