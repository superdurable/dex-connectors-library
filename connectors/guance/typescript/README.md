# Guance TypeScript connector

Read one page of dashboard UUIDs and names from the API key's workspace.

Install `@superdurable/connector-guance` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `listDashboardsPage` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
