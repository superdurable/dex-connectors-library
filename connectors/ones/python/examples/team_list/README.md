# ones listTeams Flow

This runnable Flow uses the static `ones` connection and stores the read-only provider result in Dex. Run `uv run python -m examples.team_list.worker` after configuring the connection in Dex Web. Start `OnesListTeams` with {}.

Generate strict FDG 2.0 without opening a browser:

```bash
dexcli visualize ./examples/team_list/flow.py --schema-version 2.0 --json --open=false --out /tmp/ones-python-fdg
```

The integration test exercises a real Dex Worker and Client with a fake provider. Live provider authorization and responses remain unverified.
