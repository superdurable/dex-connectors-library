# West Python connector

`dex-connector-west` exposes the `list_domains` Query Step. Configure the static `west` connection with the [provider guide](../README.md).

The [runnable Flow](examples/list_domains/) includes a Worker entrypoint and real Dex retry integration test with synthetic provider responses. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/west-python-fdg --python .venv/bin/python examples/list_domains/flow.py
```
