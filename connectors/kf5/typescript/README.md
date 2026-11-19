# KF5 TypeScript connector

Read non-content status metadata for one KF5 ticket with `getTicket`. Install `@superdurable/connector-kf5` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the tenant subdomain, agent email, and API token in Dex Web Connections. Email and token stay outside Flow input and output. HTTP 404 selects the optional `noData` branch. Local conformance tests replay Go provider scenarios; live provider authorization has not been verified.
