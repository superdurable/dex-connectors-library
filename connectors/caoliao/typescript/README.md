# Caoliao TypeScript connector

List one page of organization QR-code names and coding identifiers without scanned records.

Install `@superdurable/connector-caoliao` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `listQRCodes` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
