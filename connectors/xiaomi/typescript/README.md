# Xiaomi TypeScript connector

`@superdurable/connector-xiaomi` provides the read-only `listModels` Dex Step. It lists at most fifty model IDs visible to a Xiaomi Key through [GET /v1/models](https://mimo.mi.com/docs/zh-CN/quick-start/faq/api-integration).

Use the [runnable Flow and Worker](examples/model-list/) with the static `xiaomi` connection. In Dex Web Connections, sign in at [Xiaomi Console](https://platform.xiaomimimo.com), open API Keys, create a Key, and copy its secret into the connection form. The Key is resolved per call and never enters Flow state. The endpoint is fixed; timeout and response size have manifest defaults.

The checked-in conformance scenarios replay Go-recorded requests and outcomes. The integration test exercises a real Dex Worker and Client against a local provider that returns 503 before succeeding. Real Xiaomi authorization and model availability remain unverified without a user-controlled Key.
