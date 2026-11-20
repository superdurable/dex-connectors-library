# ZhuanqiCloud TypeScript connector

`@superdurable/connector-zhuanqicloud` provides the read-only `listModels` Dex Step. It lists at most fifty model IDs visible to a ZhuanqiCloud Key through [GET /v1/models](https://docs.zhuanqicloud.com/).

Use the [runnable Flow and Worker](examples/model-list/) with the static `zhuanqicloud` connection. In Dex Web Connections, sign in at [ZhuanqiCloud Console](https://zhuanqicloud.com), open 控制台 > API Key, create a secret key, and copy its secret into the connection form. The Key is resolved per call and never enters Flow state. The endpoint is fixed; timeout and response size have manifest defaults.

The checked-in conformance scenarios replay Go-recorded requests and outcomes. The integration test exercises a real Dex Worker and Client against a local provider that returns 503 before succeeding. Real ZhuanqiCloud authorization and model availability remain unverified without a user-controlled Key.
