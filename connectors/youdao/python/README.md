# Youdao Python connector

`dex-connector-youdao` exposes the `translate_text` Query Step. Configure the static `youdao` connection with the [provider guide](../README.md).

The [runnable Flow](examples/translate_text/) includes a Worker entrypoint and real Dex retry integration test with synthetic provider responses. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/youdao-python-fdg --python .venv/bin/python examples/translate_text/flow.py
```
