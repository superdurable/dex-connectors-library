# Yuandian Python connector

`dex-connector-yuandian` exposes the `getCompanyProfile` Query Step through `dex_connectors.yuandian`. Configure the static `yuandian` connection using the [connector guide](../README.md).

The [runnable getCompanyProfile Flow](examples/get_company_profile/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/yuandian-python-fdg --python .venv/bin/python examples/get_company_profile/flow.py
```
