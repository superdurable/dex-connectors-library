# BaishanCloud TypeScript connector

Read one CDN domain's status, type, and CNAME without exposing origin configuration.

Install `@superdurable/connector-baishancloud` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `describeDomain` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
