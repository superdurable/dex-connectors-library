# SQL Server query example

Create a Dex Web connection named `sql-server-refunds`, using the connection guidance in the connector README. Start `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS` to its Flow service address, and run `npm run example:query` for the Worker. In another terminal, run `npm run example:query:start -- examples/query-rows/input.example.json`.

The Flow reads one parameterized `SELECT` and returns the bounded row set. Its optional failure branches are deliberately unwired so a rejected query fails the Flow.
