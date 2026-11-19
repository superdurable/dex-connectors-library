# GitCode TypeScript connector

Read a GitCode repository's ID, full name, visibility, and default branch.

Install `@superdurable/connector-gitcode` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `getRepository` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
