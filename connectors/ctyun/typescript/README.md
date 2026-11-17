# CTYun Knowledge Base Q&A for Dex TypeScript

`@superdurable/connector-ctyun` lists accessible CTYun Knowledge Base Q&A resources. The `listKnowledgeBases` Query accepts `{}` and returns only IDs, names, and file counts. It selects `listed` for a successful list, including an empty list. `providerRejected`, `invalidResponse`, and `defect` are optional failure branches. Transient provider and transport failures return Retry.

## Connection

Create a Dex Web Connection named `ctyun`. In the [CTYun console](https://www.ctyun.cn/), open **账号中心 → 安全设置 → 登录保护 → 用户AccessKey → 新建** to obtain the AK and SK. In **知识库问答 → 开始使用 → 访问 → 对话页面**, find the `tenantId`. Enter all three as secret connection fields. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. The production origin is fixed to `https://kqa-global.ctapi.ctyun.cn`, as used by the [official API example](https://www.ctyun.cn/document/11032279/11084675).

The connector signs each GET `/openapi/v1/infobases/list` request with a fresh UUID and Beijing-time `eop-date`. Results exclude document contents, raw provider responses, and credentials.

## Runnable Flow

The [knowledge-base directory example](examples/knowledge-bases/README.md) includes a Worker and Client and persists the resource list. Install with `npm install`, run `npm run example:worker`, then `npm run example:start` after configuring Dex.

Provider conformance replays 15 Go-recorded scenarios, including the official signature vector. The integration test runs the same Flow through a real Dex Worker against a local provider fixture. Real CTYun authorization and resource listing remain unverified without a user-owned connection.
