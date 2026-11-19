# Apifox TypeScript connector

`@superdurable/connector-apifox` implements the read-only `getProjectAPISummary`
operation. It exports an accessible project's OpenAPI 3.1 document and returns
only the title, version, and HTTP endpoint count. The full document and access
token do not enter Dex Flow state.

[Apifox's export API](https://apifox-openapi.apifox.cn/api-173411997) documents
the endpoint, request body, and API version header. Create a personal access
token through [account settings](https://docs.apifox.com/api-access-token),
then configure the `apifox` connection in Dex Web Connections. Find the decimal
project ID in the target project's basic settings.

The [runnable project-summary Flow](examples/project-summary/) contains Worker
and Client entrypoints. Its integration test uses a fake provider and real Dex
Worker/Client. Real Apifox authorization and export responses remain unverified.
