# KokoniWorld TypeScript connector

Read up to 50 model identifiers from Kokoni World without returning inference data or model metadata.

Install `@superdurable/connector-kokoniworld` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the secret API Key in Dex Web Connections. `listModels` resolves it for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
