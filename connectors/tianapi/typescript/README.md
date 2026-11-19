# TianAPI TypeScript connector

Read one Chinese date's holiday and workday status through the official TianAPI endpoint.

Install `@superdurable/connector-tianapi` and use `newProjectConnection` with the static connection name in [`examples/basic`](examples/basic). The `getHolidayStatus` Query resolves the key per call and sends it in an HTTPS form POST body. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
