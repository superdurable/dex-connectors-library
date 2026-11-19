# SCNet TypeScript connector

`@superdurable/connector-scnet` implements the read-only `listModels` Step. It calls SCNet’s fixed `GET /api/llm/v1/models` endpoint and returns the model count and at most fifty IDs. The package excludes model details and inference.

The [runnable Flow and Worker](examples/model-list/) use a static `scnet` connection. In [SCNet Console](https://www.scnet.cn/ui/console/index.html), open Model API > API Keys, choose Create API Key, configure its team, service type, and permitted models, and copy the one-time `sk-` secret into Dex Web Connections. Leave the timeout and response-size fields blank to use their displayed defaults. The key stays in Credentials and is resolved for each call.

The Go-recorded conformance suite checks all provider outcomes and exact requests. The integration test runs a real Dex Worker and Client with a local provider that returns 503 once before success. Authorization and model availability against a real SCNet account remain unverified.
