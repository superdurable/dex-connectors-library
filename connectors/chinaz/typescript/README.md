# Chinaz TypeScript connector

Read a public site's title, keywords, description, and IP address by domain.

Install `@superdurable/connector-chinaz` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `getSiteTDK` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
