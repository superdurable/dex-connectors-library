# Haimou Python connector

`dex-connector-haimou` exposes the `lookupIPLocation` Query Step at `dex_connectors.haimou`. It accepts one public IPv4 address and returns country, province, city, district, and operator without reflecting the API key or raw provider response. Configure the secret apikey in Dex Web Connections using the [connector guide](../README.md).

The [runnable IP location Flow](examples/ip_location/) uses the static `haimou` connection. Its integration test runs a real Dex Worker and Client against a fake HTTP provider, including a 503 retry and persisted result. Live Haimou authorization, quota use, and IP lookup remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/haimou-python-fdg --python .venv/bin/python examples/ip_location/flow.py
```
