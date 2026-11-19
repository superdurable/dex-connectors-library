# Kingsoft Cloud IAM TypeScript connector

List one bounded page of Kingsoft Cloud IAM user identifiers and names.

Install `@superdurable/connector-kingsoftcloud` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `listUsers` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
