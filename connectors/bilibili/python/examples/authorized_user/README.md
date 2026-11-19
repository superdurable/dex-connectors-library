# Bilibili authorized user Flow

From `connectors/bilibili/python`, run `uv sync --group dev`, configure a Dex Web Connection named `bilibili` using the [Open Platform](https://open.bilibili.com) application and `USER_INFO` authorization, then start `dexcli dev`. Run `uv run python -m examples.authorized_user.worker` to serve the Flow. The Flow implementation is in [flow.py](flow.py). Supply the empty `GetAuthorizedUserInput` from a consuming Dex Client. The `found` branch is wired; optional outcomes fail the Flow.

Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/authorized_user/integration_test.py` to verify the same Flow with a fake provider through a real Dex Worker, Client, persistence, and 503 retry. No live provider token is required.

Generate and validate FDG 2.0 with:

```bash
dexcli visualize examples/authorized_user/flow.py --language python --schema-version 2.0 --lint app --json --out - --python .venv/bin/python
```
