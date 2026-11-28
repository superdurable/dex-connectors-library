# Baichuan AI connector

The Go connector lists knowledge bases through Baichuan AI's documented
read-only [`GET /v1/kbs` API](https://platform.baichuan-ai.com/docs/knowledgeBase).
It exposes the `ListKnowledgeBases` Query Step and a runnable
[`knowledge-base-list` Flow](go/examples/knowledge-base-list/README.md).

Install `github.com/superdurable/dex-connectors-library/connectors/baichuan/go@v0.1.0`
after the first release. The connection uses a secret API Key from an approved
[Baichuan API account](https://platform.baichuan-ai.com/). New applicants may
first need to use the platform's `申请体验` access request. The API key stays in
Dex Connections and is sent only as a Bearer header. Provider error bodies
and transport URLs are never copied into Dex results or logs.

The `listed` branch returns IDs, names, descriptions, status, attached file IDs,
and creation/update times in Unix seconds. The provider documents this endpoint
as returning all knowledge bases and defines no pagination parameters. A
successful empty list is an empty result. HTTP 408, 429, 5xx, and transport
failures retry under Dex's default one-hour budget. HTTP 401, 403, and other
non-success responses select `providerRejected`; malformed or oversized replies
select `invalidResponse`. Missing credentials select `defect`.

## Verification

```bash
cd connectors/baichuan/go
GOWORK=off go test ./...
GOWORK=off go vet ./...
```

Provider tests use a local HTTP server. The example's integration test uses a
real Dex Worker and Client with a generated mock. A real Baichuan account and
Dex Web connection are needed for live verification.

## TypeScript

The [TypeScript package](typescript/README.md) implements `listKnowledgeBases` and includes a [runnable Dex Flow](typescript/examples/knowledge-base-list/README.md). Its provider scenarios replay the Go-recorded contract; live provider authorization remains unverified without user-controlled credentials.

## Python

The [Python package](python/README.md) implements `listKnowledgeBases` with a runnable Dex Flow and Go-recorded provider scenarios. Provider authorization with a live Baichuan API key remains unverified.

## Java and Rust

Field names in provider JSON are case-insensitive. Missing or null attached-file lists become empty arrays. Name and description limits use UTF-8 bytes.

The [Java Worker](java/examples/io/superdurable/dexconnectors/baichuan/KnowledgeBaseWorker.java) and [Rust Worker](rust/examples/list_knowledge_bases.rs) run `listKnowledgeBases` with the static `baichuan` connection. Both return the ordered resource metadata from the `listed` branch. Set `DEX_FLOW_SERVICE_ADDRESS` and supply `BAICHUAN_API_KEY` to the Worker environment.

Run `./gradlew knowledgeBaseWorker` from `java/` or `cargo run --example list_knowledge_bases` from `rust/`. With a local Dex server, `./gradlew build dexDevTest` and `cargo test --all-targets` replay the Go fixtures and verify the Worker retries a temporary error and persists the returned list. Generate the Flow graphs from the connector directory:

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/baichuan/KnowledgeBaseWorker.java --schema-version 2.0 --lint app --open=false
dexcli visualize rust/examples/list_knowledge_bases.rs --schema-version 2.0 --lint app --open=false
```
