# SQL Server record-refund TypeScript example

Create `dbo.refund_ledger` with the [Go example schema](../../../go/examples/record-refund/schema.sql)
and grant the Dex SQL user `SELECT` and `INSERT` on it. Save a Dex Web connection named
`sql-server-refunds` with the guidance in the [connector README](../../../README.md).

From the TypeScript package directory, start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to
its Flow service address. Run `npm run example:execute` for the Worker. In another terminal, run
`npm run example:execute:start -- examples/record-refund/input.example.json`.

The Flow uses one constant, parameterized `INSERT` and binds Dex's stable idempotency key to the
last placeholder. It returns the committed `OUTPUT` row when inserted; a repeated call with the
same order ID inserts nothing and returns an empty row set. Optional failure branches, including
`uncertain`, are unwired, so an unresolved write fails for review instead of claiming success.
