# Moka Python connector

`dex-connector-moka` exposes the `listJobFunctions` Query Step through `dex_connectors.moka`. Configure the static `moka` connection using the [connector guide](../README.md).

The [runnable listJobFunctions Flow](examples/list_job_functions/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/moka-python-fdg --python .venv/bin/python examples/list_job_functions/flow.py
```
