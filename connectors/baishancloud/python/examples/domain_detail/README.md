# BaishanCloud domain detail Flow

From `connectors/baishancloud/python`, run `uv sync --group dev`, configure a Dex Web Connection named `baishancloud` using the [CDN API token guide](https://cdn.portal.baishan.com/track/document/api/1/1139), then start `dexcli dev`. Run `uv run python -m examples.domain_detail.worker` to serve the Flow. The Flow implementation is in [flow.py](flow.py). Supply a `DescribeDomainInput` with one ASCII CDN domain from a consuming Dex Client. The `found` branch is wired; optional outcomes fail the Flow.

Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/domain_detail/integration_test.py` to verify the same Flow with a fake provider through a real Dex Worker, Client, persistence, and 503 retry. No live provider token is required.

Generate and validate FDG 2.0 with:

```bash
dexcli visualize examples/domain_detail/flow.py --language python --schema-version 2.0 --lint app --json --out - --python .venv/bin/python
```
