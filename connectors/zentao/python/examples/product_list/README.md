# zentao listProducts Flow

This runnable Flow uses the static `zentao` connection and stores the read-only provider result in Dex. Run `uv run python -m examples.product_list.worker` after configuring the connection in Dex Web. Start `ZentaoListProducts` with {}.

Generate strict FDG 2.0 without opening a browser:

```bash
dexcli visualize ./examples/product_list/flow.py --schema-version 2.0 --json --open=false --python .venv/bin/python --out /tmp/zentao-python-fdg
```

The integration test exercises a real Dex Worker and Client with a fake provider. Live provider authorization and responses remain unverified.
