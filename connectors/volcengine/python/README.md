# Volcengine Python connector

`dex-connector-volcengine` exposes the `describeRegions` Query Step at `dex_connectors.volcengine`. It filters provider data to the Go-defined output model and never persists credentials or raw responses. Configure the static `volcengine` connection using the [connector guide](../README.md).

The [runnable describeRegions Flow](examples/describe_regions/) includes a real Dex Worker and Client integration test with a fake provider. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/volcengine-python-fdg --python .venv/bin/python examples/describe_regions/flow.py
```
