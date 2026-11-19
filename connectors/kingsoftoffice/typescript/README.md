# WPS 365 TypeScript connector

Read one enterprise drive folder page without file content.

Install `@superdurable/connector-kingsoftoffice` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `listFolderFiles` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
