# GetResponse Python contact inspection

This runnable Flow calls `listContacts` using the static `getresponse` connection, stores the page in an Attribute, and completes with the typed result. Sign in at https://app.getresponse.com/api, open Integrations and API > API, and generate or copy the API Key into the secret `api_key` field. This release supports SMB accounts; leave `baseUrl`, timeout, and response-size fields blank for their displayed defaults.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from `connectors/getresponse/python`:

```bash
uv run python -m examples.contact_inspection.worker
```

Start Flow type `GetResponseContactInspection` with a unique Flow ID and input `{"page":1,"perPage":20}`. The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected.

Generate a strict FDG 2.0 graph from this package with `dexcli visualize ./examples/contact_inspection/flow.py --language python --python .venv/bin/python --schema-version 2.0 --json --out /tmp/getresponse-python-fdg --open=false`. The graph must report `valid: true` with no diagnostics. Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8905 uv run pytest -q examples/contact_inspection/integration_test.py` against `dexcli dev` for the real Worker retry path. Live GetResponse authorization has not been tested.
