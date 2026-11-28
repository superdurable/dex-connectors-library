# CTYun Knowledge Base Q&A connector

This connector lists resource metadata from [CTYun Knowledge Base Q&A](https://www.ctyun.cn/document/11032279/11084675). It returns knowledge-base ID, name, and file count; it does not retrieve documents. CTYun's official site identifies the operator as 天翼云科技有限公司.

## Connection

In the [CTYun account center](https://www.ctyun.cn/), open **安全设置 > 登录保护 > 用户AccessKey > 新建** and copy AK and SK into the secret connection fields. In Knowledge Base Q&A console open **开始使用 > 访问**; the dialogue page shows the required `tenantId` ([official location guide](https://www.ctyun.cn/document/11032279/11074496)). The credentials must have access to that tenant. Leave transport settings blank for their documented defaults.

The connector signs each GET request with [CTYun EOP authentication](https://www.ctyun.cn/document/11032279/11075051), using a fresh UUID and Beijing-local `eop-date`. It never puts AK/SK, tenant ID, or signature in the Flow result. It retries transport, 429, 5xx, and the documented temporary `40002`/`40004` failures. Other provider business errors use optional branches; an unwired optional branch fails the Flow.

The [runnable Dex Flow](go/examples/knowledge-base-directory/README.md) uses the static `ctyun` connection and a synthetic mock. Live provider authorization and tenant access require user-controlled CTYun credentials and have not been verified here.

## Python

The [Python package](python/README.md) implements the same read-only `listKnowledgeBases` operation and includes a [runnable Dex Flow](python/examples/knowledge_base_directory/README.md). Its 15 provider scenarios replay Go-recorded requests, outputs, and retry decisions. No live CTYun account has been authorized for this port.

## TypeScript

The [TypeScript package](typescript/README.md) implements the same signed `listKnowledgeBases` request. Its 15 Go-recorded provider scenarios and runnable Flow verify retry and persisted output through a real Dex Worker and Client.

## Java and Rust

Provider field names are case-insensitive. A missing or null file count is returned as zero.

The [Java Worker](java/examples/io/superdurable/dexconnectors/ctyun/KnowledgeBaseWorker.java) and [Rust Worker](rust/examples/list_knowledge_bases.rs) run `listKnowledgeBases` with the static `ctyun` connection. Both return the ordered resource metadata from the `listed` branch. Set `DEX_FLOW_SERVICE_ADDRESS` and supply `CTYUN_ACCESS_KEY`, `CTYUN_SECURITY_KEY` and `CTYUN_TENANT_ID` to the Worker environment.

Run `./gradlew knowledgeBaseWorker` from `java/` or `cargo run --example list_knowledge_bases` from `rust/`. With a local Dex server, `./gradlew build dexDevTest` and `cargo test --all-targets` replay the Go fixtures and verify the Worker retries a temporary error and persists the returned list. Generate the Flow graphs from the connector directory:

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/ctyun/KnowledgeBaseWorker.java --schema-version 2.0 --lint app --open=false
dexcli visualize rust/examples/list_knowledge_bases.rs --schema-version 2.0 --lint app --open=false
```
