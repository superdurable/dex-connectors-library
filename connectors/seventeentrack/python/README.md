# 17TRACK registered shipments Python connector

`dex-connector-seventeentrack` exposes the provider Query at `dex_connectors.seventeentrack`. The result contains one bounded page of tracking statuses. Configure credentials in Dex Web Connections using the [connector guide](../README.md).

The [runnable Flow](examples/shipment_list/) uses the static `seventeentrack` connection. Its integration test runs a real Dex Worker and Client against a fake provider, including a 503 retry and persisted result. Live provider authorization and API behavior remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/seventeentrack-python-fdg --python .venv/bin/python examples/shipment_list/flow.py
```
