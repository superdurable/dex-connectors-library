# eSign signing flow status

`GetSignFlowStatusFlow` calls `getSignFlowStatus` through the static `esign` Dex Web connection, persists only the requested flow ID and numeric status, and completes with them. The optional `providerRejected`, `invalidResponse`, and `defect` branches are unwired, so selecting one fails the Flow. Temporary failures retry under the Dex Step policy.

Create a connection named `esign` in Dex Web. At the [eSign developer console](https://open.esign.cn/my-apps/home), open **正式服务 → 应用管理 → 我的应用**, select the production app, and copy its AppID to `app_id` and secret AppSecret to `app_secret`. Leave `timeout` (Go duration) and `maxResponseBytes` (bytes) blank for displayed defaults. There are no operation configuration fields. Supply an existing `signFlowId` from your signing workflow or flow list.

From `connectors/esign/python`, run `uv sync --locked`, start `dexcli dev -open=false`, and run `uv run python -m examples.get_sign_flow_status.worker` with the Dex project connection environment. Generate strict FDG 2.0 with:

```sh
uv run dexcli visualize examples/get_sign_flow_status/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/dex-esign-python-flow-status-fdg --open=false --python .venv/bin/python
```

With `DEX_FLOW_SERVICE_ADDRESS` pointing at a running Dex development server, `uv run pytest examples/get_sign_flow_status/integration_test.py` starts the Flow through a real Worker and Client, retries a synthetic provider 503, and checks the signed request and filtered output. It uses no real eSign credentials. Live authorization and signing status remain unverified.
