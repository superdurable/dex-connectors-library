# 51Tracking TypeScript connector

Read aggregate registered parcel counts by delivery status without exposing tracking numbers.

Install `@superdurable/connector-fiftyonetracking` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `getShipmentStatusCounts` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
