# Worktile Python connector

`dex-connector-worktile` exposes the `listProjects` Query Step through `dex_connectors.worktile`. Configure the static `worktile` connection using the [connector guide](../README.md).

The [runnable listProjects Flow](examples/list_projects/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/worktile-python-fdg --python .venv/bin/python examples/list_projects/flow.py
```
