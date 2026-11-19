# Honor TypeScript connector

Look up the numeric app ID of an Android package owned by the HONOR developer account.

Install `@superdurable/connector-honor` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `lookupAppID` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
