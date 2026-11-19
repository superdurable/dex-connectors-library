# Dimleap TypeScript connector

Read at most 50 model IDs available to a Dimleap API Key without invoking inference.

Install `@superdurable/connector-dimleap` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `listModels` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
