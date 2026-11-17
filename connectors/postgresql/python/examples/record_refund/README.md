# Record a refund

This runnable Flow uses the static Dex Web connection `postgresql-ledger`. It queries `refund_ledger` for an existing order, then inserts a refund with the stable Dex Step idempotency key through `execute`, which retries a COMMIT with an unknown outcome. It then adds the new refund to today's total in `refund_daily_totals` through `executeNonIdempotent`, because running that addition twice would count the refund twice. Configure the table using [`schema.sql`](../../../go/examples/record-refund/schema.sql) and enter a dedicated database role in Dex Web Connections using the [connector setup guide](../../../README.md).

From the repository root, install the package and generate strict FDG 2.0:

```sh
uv sync --project connectors/postgresql/python
mkdir -p /tmp/postgresql-python-refund/graphs
dexcli visualize connectors/postgresql/python/examples/record_refund/flow.py \
  --schema-version 2.0 --json \
  --python connectors/postgresql/python/.venv/bin/python \
  --out /tmp/postgresql-python-refund/graphs/record-refund
```

Start `dexcli dev` with free local ports and `--flow-rendering-dir /tmp/postgresql-python-refund/graphs`, set `DEX_FLOW_SERVICE_ADDRESS` to that Dex gRPC address, then run `uv run python -m examples.record_refund.worker` from `connectors/postgresql/python`. The `integration_test.py` file runs the same Flow with generated mock operations on a real Dex Worker and Client.

The example wires only the happy-path branches. An optional branch fails the Flow when selected, so production applications should wire and handle any business outcome they need. An `uncertain` daily total fails the Flow for review instead of counting the refund twice; inspect `refund_daily_totals` before correcting it.
