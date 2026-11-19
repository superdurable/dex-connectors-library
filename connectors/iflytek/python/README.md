# iFLYTEK Spark Python connector

`dex-connector-iflytek` exposes the `createChatCompletion` Query Step at `dex_connectors.iflytek`. It sends one bounded prompt and returns assistant content and token counts without reflecting APIPassword or raw provider data. Configure the secret APIPassword and enabled model in Dex Web Connections using the [connector guide](../README.md).

The [runnable chat Flow](examples/chat_completion/) uses the static `iflytek` connection. Its integration test runs a real Dex Worker and Client against a fake HTTP provider, including a 503 retry and persisted result. Live Spark authorization, billing, and generation remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/iflytek-python-fdg --python .venv/bin/python examples/chat_completion/flow.py
```
