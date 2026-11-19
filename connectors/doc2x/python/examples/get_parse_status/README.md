# Doc2X PDF parse status

`GetParseStatusFlow` calls `getParseStatus` through the static `doc2x` Dex Web connection, persists the task state and completion percentage Attribute, and completes with it. The optional `providerRejected`, `invalidResponse`, and `defect` branches are unwired, so selecting one fails the Flow. Temporary errors retry under the Dex Step policy.

Create a connection named `doc2x` in Dex Web. At the [Doc2X Open Platform](https://open.noedgeai.com), open **API Keys**, create or select a platform API Key, and copy it to `apiKey`. Do not use a desktop login token. Leave `timeout` (Go duration) and `maxResponseBytes` (bytes) blank for their displayed defaults. There are no operation configuration fields. Supply `uid` from a prior PDF preupload response.

From `connectors/doc2x/python`, run `uv sync --locked`, start `dexcli dev`, and run `uv run python -m examples.get_parse_status.worker` with the Dex project connection environment. Generate strict FDG 2.0 with:

```sh
uv run dexcli visualize examples/get_parse_status/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/dex-doc2x-python-parse-status-fdg --open=false --python .venv/bin/python
```

With `DEX_FLOW_SERVICE_ADDRESS` pointing at a running Dex development server, `uv run pytest examples/get_parse_status/integration_test.py` starts the Flow through a real Worker and Client, retries a synthetic provider 503, and checks the filtered output. It uses no real API Key. Live authorization and PDF task state remain unverified.
