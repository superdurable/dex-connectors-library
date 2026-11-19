# Guance Python connector

`dex-connector-guance` exposes the `listDashboardsPage` Query Step at `dex_connectors.guance`. The connector returns dashboard UUIDs, names, and pagination without user details. See the [Guance console](https://console.guance.com/) for workspace API-key setup.

The [runnable Flow](examples/dashboard_page/) uses the static `guance` connection. Its integration test runs a real Dex Worker and Client against a fake provider, including a 503 retry. Live Guance authorization and dashboard-list behavior remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/guance-python-fdg --python .venv/bin/python examples/dashboard_page/flow.py
```
