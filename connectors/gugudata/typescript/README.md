# GuGuData TypeScript connector

Segment short Chinese text through the GuGuData NLP API and return the provider's segmented text.

Install `@superdurable/connector-gugudata` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `segmentText` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
