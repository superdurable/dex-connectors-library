# GuGuData Python connector

`dex-connector-gugudata` exposes the `segmentText` Query Step at `dex_connectors.gugudata`. It accepts short text of at most 2000 UTF-16 code units and returns the provider's comma-separated segmentation without reflecting the AppKey or raw response. Configure the secret AppKey in Dex Web Connections using the [connector guide](../README.md).

The [runnable segmentation Flow](examples/segment_text/) uses the static `gugudata` connection. Its integration test runs a real Dex Worker and Client against a fake HTTP provider, including a 503 retry and persisted result. Live GuGuData authorization, quota use, and segmentation remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/gugudata-python-fdg --python .venv/bin/python examples/segment_text/flow.py
```
