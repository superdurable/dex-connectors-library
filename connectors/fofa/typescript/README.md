# Fofa TypeScript connector

Search FOFA's public domain asset index and return up to five host, IP, and port records.

Install `@superdurable/connector-fofa` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `searchDomainAssets` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
