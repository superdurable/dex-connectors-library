# Feihan TypeScript connector

Read one known Feihan recharge order status without exposing recharge account details.

Install `@superdurable/connector-feihan` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `queryRechargeOrder` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
