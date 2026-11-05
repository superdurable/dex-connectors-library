# Record a refund

This Flow looks up an order in `refund_ledger`, then inserts one refund with
the Dex Call ID bound to its final SQL placeholder. Its static connection name
is `postgresql-ledger`. The password stays in the Worker's environment and
never enters the Flow input or result. The starter accepts the checked-in
`input.example.json` shape.

Create a disposable PostgreSQL database and apply the Go example's
[`schema.sql`](../../../go/examples/record-refund/schema.sql). Set
`POSTGRESQL_CONNECTOR_TEST_HOST`, `POSTGRESQL_CONNECTOR_TEST_PORT`,
`POSTGRESQL_CONNECTOR_TEST_DATABASE`, `POSTGRESQL_CONNECTOR_TEST_USER`,
`POSTGRESQL_CONNECTOR_TEST_PASSWORD`, and
`POSTGRESQL_CONNECTOR_TEST_SSL_MODE` for the Worker. Use a role with only the
permissions needed for the ledger.

Start `dexcli dev` on the desired ports and set `DEX_FLOW_SERVICE_ADDRESS` to
its gRPC address. From `connectors/postgresql/typescript`, run the Worker and
starter in separate terminals:

```sh
npm run example:refund
npm run example:refund:start -- examples/record-refund/input.example.json
```

The generated mock drives the real Dex integration test without a local
database:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:23651 npm run test:dex
```

For strict Flow Definition Graph 2.0 validation, build the package, install it
locally under this example for the CLI analyzer, and visualize the Flow:

```sh
npm run build
npm install --prefix examples/record-refund --no-save --no-package-lock --ignore-scripts .
dexcli visualize examples/record-refund/flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

The direct native-driver integration suite runs in CI's PostgreSQL service
container. Live provider authorization and the full Worker-to-database path
have not been exercised outside that CI path.
