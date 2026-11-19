# Jiguang application tag Flow

From `connectors/jiguang/python`, run `uv sync --group dev`, configure the Dex Web Connection `jiguang` using the [Jiguang application guide](https://www.jiguang.cn), and start `dexcli dev -open=false`. Run `uv run python -m examples.tag_read.worker` to serve the Flow. Start `ApplicationTagsFlow` with `ListApplicationTagsInput()`. The `listed` branch completes the Flow; optional outcomes fail it.

Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/tag_read/integration_test.py` to verify the same Flow with a fake provider through a real Dex Worker, Client, persistence, and 503 retry.

Generate and validate FDG 2.0 with:

```bash
dexcli visualize examples/tag_read/flow.py --language python --schema-version 2.0 --lint app --json --out - --python .venv/bin/python --open=false
```
