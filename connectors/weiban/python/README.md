# Weiban Python connector

`dex-connector-weiban` exposes the `listTags` Query Step through `dex_connectors.weiban`. Configure the static `weiban` connection using the [connector guide](../README.md).

The [runnable listTags Flow](examples/list_tags/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/weiban-python-fdg --python .venv/bin/python examples/list_tags/flow.py
```
