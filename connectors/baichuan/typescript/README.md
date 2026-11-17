# Baichuan TypeScript connector

`@superdurable/connector-baichuan` provides the read-only `listKnowledgeBases` Dex Step. It reads the knowledge-base list through Baichuan AI's [documented API](https://platform.baichuan-ai.com/docs/knowledgeBase).

Use the [runnable Flow and Worker](examples/knowledge-base-list/) with a static `baichuan` connection. In Dex Web Connections, sign in at [Baichuan AI Platform](https://platform.baichuan-ai.com/), request access if required, then open API Key > 创建 API Key and save the secret key in the connection form. The key is resolved for each call and never enters Flow state. The endpoint, timeout, and response limit have manifest defaults.

The checked-in conformance scenarios replay Go-recorded requests and outcomes. The integration test exercises a real Dex Worker and Client with a local provider that returns 503 before succeeding. Real Baichuan authorization and knowledge-base discovery remain unverified without a user-controlled approved account.
