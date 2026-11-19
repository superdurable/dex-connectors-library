# Qingflow TypeScript connector

`@superdurable/connector-qingflow` implements the read-only `listApplications` Step. It calls Qingflow’s fixed `GET /app` endpoint and returns only application keys and names from the token’s workspace. It never reads application records or form contents.

The [runnable Flow and Worker](examples/model-list/) use the static `qingflow` connection. At [Qingflow](https://qingflow.com/), sign in as a workspace administrator, then open 轻商城 > 插件中心 > 第三方连接 > OPEN API > 配置 and copy the secret super-admin `accessToken` into Dex Web Connections. A permission-group token cannot list workspace applications. Leave timeout and response-size fields blank for their displayed defaults. Credentials are resolved per request and stay out of Flow state.

The Go-recorded conformance suite checks exact requests and all provider outcomes. The integration test uses a real Dex Worker and Client with a local provider that returns 503 once before success. Authorization and application visibility in a real Qingflow workspace remain unverified.
