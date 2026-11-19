# OceanBase Cloud TypeScript connector

Read one bounded page of OceanBase Cloud cluster instances using HTTP Digest authentication. Install `@superdurable/connector-oceanbase` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure AccessKey ID, AccessKey Secret, Project ID, and service site in Dex Web Connections. The client resolves credentials for each call and returns only instance ID, name, state, region, pagination, and total count.

The conformance test replays all Go-recorded Digest and failure scenarios. The real Dex Worker/Client integration test covers 503 retry, Digest challenge, and durable result persistence with a synthetic provider. Live OceanBase authorization and responses remain unverified.
