# SQL Server record-refund TypeScript example

Create `dbo.refund_ledger` and `dbo.refund_daily_totals` with the
[Go example schema](../../../go/examples/record-refund/schema.sql), and grant the Dex SQL user
`SELECT` and `INSERT` on the ledger and `SELECT`, `INSERT`, and `UPDATE` on the daily totals. Save a Dex Web connection named
`sql-server-refunds` with the guidance in the [connector README](../../../README.md).

From the TypeScript package directory, start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to
its Flow service address. Run `npm run example:execute` for the Worker. In another terminal, run
`npm run example:execute:start -- examples/record-refund/input.example.json`.

The Flow uses one constant, parameterized `INSERT` and binds Dex's stable idempotency key to the
last placeholder. `execute` is repeat-safe, so a COMMIT with an unknown outcome runs the same
`INSERT` again with the same key. It returns the committed `OUTPUT` row when inserted; a repeated
call with the same order ID inserts nothing and returns an empty row set. When the insert returned
a row, the Flow adds the refund to today's total in `dbo.refund_daily_totals` with
`executeNonIdempotent`, because running that `MERGE` twice would count the refund twice. A replay
whose earlier COMMIT applied returns no row and skips the daily total; the Go example reads the row
back to tell such a replay apart. Optional failure branches, including the daily total's
`uncertain`, are unwired, so an unresolved write fails for review instead of claiming success.
