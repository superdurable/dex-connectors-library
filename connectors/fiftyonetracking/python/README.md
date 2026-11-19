# 51Tracking Python connector

`dex-connector-fiftyonetracking` exposes the `getShipmentStatusCounts` Query Step at `dex_connectors.fiftyonetracking`. See the [51Tracking API guide](https://docs.51tracking.com/article/12) for API-key setup. The connection uses the static name `fiftyonetracking`.

The [runnable Flow](examples/status_counts/) records only aggregate status counts. Its integration test runs a real Dex Worker and Client against a fake provider, including a 503 retry. Live 51Tracking authorization and response behavior remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/fiftyonetracking-python-fdg --python .venv/bin/python examples/status_counts/flow.py
```
