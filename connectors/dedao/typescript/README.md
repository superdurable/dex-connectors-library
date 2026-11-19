# Dedao TypeScript connector

Read an existing Get Note task's status and resulting note ID without loading note content.

Install `@superdurable/connector-dedao` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `getNoteTaskStatus` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
