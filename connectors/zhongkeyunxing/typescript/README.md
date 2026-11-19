# ZhongkeYunxing TypeScript connector

`@superdurable/connector-zhongkeyunxing` provides the read-only `listModels` Dex Step. It lists at most fifty model IDs visible to a ZhongkeYunxing Key through [GET /v1/models](https://www.zhongkeyunx.com/brand/guide.html).

Use the [runnable Flow and Worker](examples/model-list/) with the static `zhongkeyunxing` connection. In Dex Web Connections, sign in at [ZhongkeYunxing Console](https://www.zhongkeyunx.com), open the platform console, create an API Key, and copy its secret into the connection form. The Key is resolved per call and never enters Flow state. The endpoint is fixed; timeout and response size have manifest defaults.

The checked-in conformance scenarios replay Go-recorded requests and outcomes. The integration test exercises a real Dex Worker and Client against a local provider that returns 503 before succeeding. Real ZhongkeYunxing authorization and model availability remain unverified without a user-controlled Key.
