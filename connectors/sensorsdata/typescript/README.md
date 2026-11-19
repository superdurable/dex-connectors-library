# Sensors Data TypeScript connector

Read one campaign page and its pagination totals.

Install `@superdurable/connector-sensorsdata` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `listCampaigns` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
