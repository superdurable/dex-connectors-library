# ThreatBook Python connector

`dex-connector-threatbook` exposes the `checkIPReputation` Query Step at `dex_connectors.threatbook`. It filters provider data to the Go-defined output model and never persists credentials or raw responses. Configure the static `threatbook` connection using the [connector guide](../README.md).

The [runnable checkIPReputation Flow](examples/check_ip_reputation/) includes a real Dex Worker and Client integration test with a fake provider. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/threatbook-python-fdg --python .venv/bin/python examples/check_ip_reputation/flow.py
```
