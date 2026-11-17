# 365 API TypeScript connector

`@superdurable/connector-365api` provides the read-only `listModels` Dex Step. It lists at most fifty model IDs visible to a 365 API Key through [GET /v1/models](https://www.hby365.studio/developers).

Use the [runnable Flow and Worker](examples/model-list/) with the static `hby365` connection. In Dex Web Connections, sign in at [365 API Console](https://www.hby365.studio/console), open API Keys, create a Key, and copy its secret into the connection form. The Key is resolved per call and never enters Flow state. The endpoint is fixed; timeout and response size have manifest defaults.

The checked-in conformance scenarios replay Go-recorded requests and outcomes. The integration test exercises a real Dex Worker and Client against a local provider that returns 503 before succeeding. Real 365 API authorization and model availability remain unverified without a user-controlled Key.
