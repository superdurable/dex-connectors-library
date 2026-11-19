# Jiguang JPush TypeScript connector

Read application tag names without device identifiers or push delivery.

Install `@superdurable/connector-jiguang` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `listApplicationTags` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
