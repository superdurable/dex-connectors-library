# Record a refund with SQL Server

Create `dbo.refund_ledger` with `schema.sql` using an administrator connection. Create the
`sqlserver-refunds` connection in Dex Web with SQL authentication and the required SELECT and INSERT
permissions. Run `uv run --project connectors/microsoft/sql-server/python python -m examples.record_refund.worker`
with `DEX_FLOW_SERVICE_ADDRESS` pointing to the Dex server, then start a Flow with
`uv run --project connectors/microsoft/sql-server/python python -m examples.record_refund.start
<unique-flow-id> connectors/microsoft/sql-server/python/examples/record_refund/sample-input.json`.

The write binds the stable Dex Call ID as its final unique key, and the subsequent read returns the
committed row. Optional branches remain unwired so the Flow fails on a rejected or uncertain write.
From the repository root, generate and lint strict Flow Definition Graph 2.0:

```bash
PYTHONPATH=connectors/microsoft/sql-server/python dexcli visualize \
  connectors/microsoft/sql-server/python/examples/record_refund/flow.py \
  --language python --schema-version 2.0 --lint app --json \
  --python connectors/microsoft/sql-server/python/.venv/bin/python \
  --out /tmp/sqlserver-python-fdg/record-refund
```

The graph shows PrepareRefund, WriteRefund, PrepareReadBack, ReadRefund, and CompleteRefund.
