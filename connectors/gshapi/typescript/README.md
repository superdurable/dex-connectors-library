# GSH API TypeScript connector

Read the count and first fifty model IDs visible to the connected GSH API key.

Install `@superdurable/connector-gshapi` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `listModels` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
