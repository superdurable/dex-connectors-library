# CTYun Knowledge Base Q&A Python connector

`dex-connector-ctyun` implements the read-only `listKnowledgeBases` Query for Dex Flows. It returns each accessible knowledge base's ID, name, and file count. The request follows the [CTYun list API](https://www.ctyun.cn/document/11032279/11084675): signed `GET /openapi/v1/infobases/list` to `kqa-global.ctapi.ctyun.cn`. The document's request table spells the host `koa`, while its curl example and the existing Go connector use `kqa`.

## Connection

Create the static `ctyun` connection in Dex Web. In the [CTYun console](https://www.ctyun.cn/), open **账号中心 > 安全设置 > 登录保护 > 用户AccessKey > 新建**. Copy the AK to `access_key` and its paired SK to `security_key`; both are secret. In **知识库问答 > 开始使用 > 访问**, find the displayed `tenantId` and copy it to secret `tenant_id` ([provider guide](https://www.ctyun.cn/document/11032279/11074496)). All three fields are required; blank prevents authorization. Grant the AK access to that tenant.

Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. The origin is restricted to the documented HTTPS host. Timeout is measured in seconds in Python; response size is measured in bytes. Override only when the provider changes its endpoint or a measured read needs a different limit.

The connector signs every request with a fresh UUID, current Beijing-local `eop-date`, and `Eop-Authorization`. AK, SK, tenant ID, and signature never enter the Flow output. HTTP 429/5xx, transport failures, and documented temporary business statuses retry; definite rejections select optional `providerRejected`, malformed or oversized responses select `invalidResponse`, and missing credentials select `defect`. Only `listed` is wired in the example, so optional branches fail the Flow.

## Runnable Flow

[knowledge_base_directory](examples/knowledge_base_directory/README.md) includes a Worker, start command, and real Dex integration test. It stores the returned summaries in a registered Attribute. The integration test uses inert credentials and a fake provider over a real Dex Worker and Client. No live CTYun account authorization or tenant access has been verified.
