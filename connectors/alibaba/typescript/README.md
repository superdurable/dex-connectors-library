# Alibaba Cloud TypeScript connector

`@superdurable/connector-alibaba` provides the read-only `describeRegions` Dex Step. It signs Alibaba Cloud ECS OpenAPI requests with [ACS3-HMAC-SHA256](https://help.aliyun.com/en/sdk/product-overview/v3-request-structure-and-signature) and returns the region catalog.

Use the [runnable Flow and Worker](examples/regions/) with the static `alibaba` connection. In Dex Web Connections, create or select a RAM user at [Alibaba Cloud RAM](https://ram.console.aliyun.com) > Identities > Users > Authentication > AccessKey. Enter its AccessKey ID and one-time secret in the connection form. The secret is resolved for each request and never enters Flow state. The ECS endpoint, timeout, and response size limit have manifest defaults.

The checked-in conformance scenarios replay Go-recorded requests and outcomes, including exact signatures. The integration test exercises a real Dex Worker and Client with a local ECS provider that returns 503 before succeeding. Real Alibaba Cloud authorization and region discovery remain unverified without user-controlled RAM credentials.
