# yunpian getAccountBalance Flow

This runnable Flow uses the static `yunpian` connection and stores the read-only provider result in Dex. Run `uv run python -m examples.account_balance.worker` after configuring the connection in Dex Web. Start `YunpianGetAccountBalance` with {}.

Generate strict FDG 2.0 without opening a browser:

```bash
dexcli visualize ./examples/account_balance/flow.py --schema-version 2.0 --json --open=false --python .venv/bin/python --out /tmp/yunpian-python-fdg
```

The integration test exercises a real Dex Worker and Client with a fake provider. Live provider authorization and responses remain unverified.
