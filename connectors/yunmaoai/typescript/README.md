# YunMaoAI TypeScript connector

`@superdurable/connector-yunmaoai` provides the read-only `listModels` Dex Step. It lists at most fifty model IDs visible to a YunMaoAI Key through [GET /v1/models](https://yunmaoai.com/docs.html).

Use the [runnable Flow and Worker](examples/model-list/) with the static `yunmaoai` connection. In Dex Web Connections, sign in at [YunMaoAI Console](https://api.yunmaoai.com/sign-up), open the console’s 令牌 or API 密钥 page, create a Key, and copy its secret into the connection form. The Key is resolved per call and never enters Flow state. The endpoint is fixed; timeout and response size have manifest defaults.

The checked-in conformance scenarios replay Go-recorded requests and outcomes. The integration test exercises a real Dex Worker and Client against a local provider that returns 503 before succeeding. Real YunMaoAI authorization and model availability remain unverified without a user-controlled Key.
