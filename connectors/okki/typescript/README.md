# Okki TypeScript connector

Look up country, province, city, district, and network operator for one public IPv4 address.

Install `@superdurable/connector-okki` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `listCustomerFields` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
