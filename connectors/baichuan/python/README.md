# Baichuan Python connector

`dex-connector-baichuan` exposes `listKnowledgeBases` as a Dex Query Step. The [official knowledge-base API](https://platform.baichuan-ai.com/docs/knowledgeBase) documents the `GET /v1/kbs` endpoint and Bearer authorization. Configure a secret API key in Dex Web Connections; the key never enters Flow state.

See the [runnable Flow](examples/knowledge_base_list/) for a Worker and static connection name. The Go-recorded conformance scenarios verify exact requests, output, branches, and retries. Real Baichuan authorization has not been verified locally.
