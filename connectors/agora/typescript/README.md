# Agora TypeScript connector

`@superdurable/connector-agora` provides the read-only `listRunningAgents` Dex Step. It lists one bounded page of currently running Conversational AI agents using the [Agora REST API](https://docs.agora.io/en/api-reference/api-ref/conversational-ai/list).

Use the [runnable Flow and Worker](examples/agent-list/) with a named `agora` connection. In Dex Web Connections, copy the project App ID from Agora Console > Projects > Credentials. Create and download a RESTful API Key under the account menu > RESTful API Keys; enter its Customer ID and one-time Customer Secret. The secret is resolved for each call and never enters Flow state. The endpoint, timeout, and response limit have manifest defaults.

The checked-in conformance scenarios replay Go-recorded requests and outcomes. The example's integration test exercises a real Dex Worker and Client with a local provider that returns 503 before succeeding. Real Agora authorization and current agent discovery remain unverified without a user-controlled project.
