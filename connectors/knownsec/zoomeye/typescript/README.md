# ZoomEye TypeScript connector

Query the connected ZoomEye account's subscription plan and remaining point balances with `getAccountQuota`.

Install `@superdurable/connector-zoomeye` and use `newConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the API key in Dex Web Connections. The API key stays in the connection store; the Flow returns only subscription data. Local conformance tests replay the Go provider scenarios. Live provider authorization has not been verified.
