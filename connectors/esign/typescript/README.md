# eSign TypeScript connector

Read the numeric status of an existing eSign signing flow without returning document or signer details.

Install `@superdurable/connector-esign` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `getSignFlowStatus` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
