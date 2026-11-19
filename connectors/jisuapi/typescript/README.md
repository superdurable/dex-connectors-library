# JisuAPI TypeScript connector

Read province identifiers and names from JisuAPI without returning other provider data.

Install `@superdurable/connector-jisuapi` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the appkey in Dex Web Connections. `listProvinces` resolves it for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
