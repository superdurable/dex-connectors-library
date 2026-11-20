# Useasy Python connector

`dex-connector-useasy` exposes the `listAgents` Query Step through `dex_connectors.useasy`. Configure the static `useasy` connection using the [connector guide](../README.md).

The [runnable listAgents Flow](examples/list_agents/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/useasy-python-fdg --python .venv/bin/python examples/list_agents/flow.py
```
