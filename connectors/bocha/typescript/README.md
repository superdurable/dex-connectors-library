# Bocha TypeScript connector

Search public web pages and return at most five titles, HTTPS URLs, and snippets.

Install `@superdurable/connector-bocha` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `searchWeb` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
