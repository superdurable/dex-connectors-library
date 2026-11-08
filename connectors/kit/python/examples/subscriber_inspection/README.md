# Kit Python subscriber inspection

This runnable Flow calls `listSubscribers` using the static `kit` connection, stores the page in an Attribute, and completes with the typed result. Open https://app.kit.com/account_settings/developer_settings > Settings > Developer > V4 API Keys, add a new key, and copy it into the secret `api_key` field. Leave `baseUrl`, timeout, and response-size fields blank for their displayed defaults.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from `connectors/kit/python`:

```bash
uv run python -m examples.subscriber_inspection.worker
```

Start Flow type `KitSubscriberInspection` with a unique Flow ID and input `{"perPage":20}`. The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected.

Generate a strict FDG 2.0 graph from this package with `dexcli visualize ./examples/subscriber_inspection/flow.py --language python --python .venv/bin/python --schema-version 2.0 --json --out /tmp/kit-python-fdg --open=false`. The graph must report `valid: true` with no diagnostics. Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8905 uv run pytest -q examples/subscriber_inspection/integration_test.py` against `dexcli dev` for the real Worker retry path. Live Kit authorization has not been tested.
