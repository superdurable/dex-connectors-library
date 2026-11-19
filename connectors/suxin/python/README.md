# Suxin Python connector

`dex-connector-suxin` exposes the `listOrganizations` Query Step at `dex_connectors.suxin`. It filters provider data to the Go-defined output model and never persists credentials or raw responses. Configure the static `suxin` connection using the [connector guide](../README.md).

The [runnable listOrganizations Flow](examples/list_organizations/) includes a real Dex Worker and Client integration test with a fake provider. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/suxin-python-fdg --python .venv/bin/python examples/list_organizations/flow.py
```
