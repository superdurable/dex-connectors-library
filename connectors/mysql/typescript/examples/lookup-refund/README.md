# Refund ledger lookup

This runnable Flow looks up a refund by `order_id` using one prepared,
read-only MySQL query. Set up the MySQL connector in Dex Web with the static
connection name `mysql-refunds`, create `refund_ledger` with `order_id` and
`amount_usd` columns, and give the account `SELECT` permission. Set
`DEX_FLOW_SERVICE_ADDRESS` to your `dexcli dev` server address.

Run `npm run example:lookup-refund` for the Worker and
`npm run example:lookup-refund:start -- 88213` in another terminal to start a
Flow. The output is a bounded `RowSet`. The checked-in Dex integration test
uses the same Flow with a scripted provider to verify retry and unwired
rejection behavior across a real Worker and Client.
