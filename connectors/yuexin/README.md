# Yuexin AI Gateway connector

`ListModels` reads IDs available to a Yuexin AI Gateway API Key. The official [Models API reference](https://app.yuexin.ai/docs/api/reference/models) documents `GET /v1/models`, Bearer authentication, and the OpenAI-compatible `object: list` / `data[].id` response. Output contains the provider model count and at most the first 50 IDs. Inference, model metadata and account data are excluded.

## Authorization

Sign in to the [Yuexin console](https://app.yuexin.ai/keys), open API Keys, create a Key, set its name and optional restrictions, and save the one-time secret into Dex Web Connections. The [official key guide](https://app.yuexin.ai/docs/api-keys) explains the exact path and options. Leave timeout and maximum response size blank for the displayed defaults.

## Example

The [runnable Flow](go/examples/list-models/) starts with `{}` and completes with a typed `ListModelsOutput`. Its default mock returns one model. Live Yuexin authorization, model visibility and provider response variation remain unverified without a user-controlled Dex Web Connection.

## Python

The `python/` package provides the `listModels` Query. Run its checked-in example from `python/examples/`.

The [TypeScript package](typescript/README.md) implements `listModels` with a [runnable Dex Flow](typescript/examples/model-list/README.md). Its provider scenarios replay the Go-recorded contract; live authorization remains unverified without user-controlled credentials.

The [Java Worker](java/examples/io/superdurable/dexconnectors/yuexin/ModelCatalogWorker.java) and
[Rust Worker](rust/examples/list_models.rs) serve the same model-list Query.
Both examples read the key from `YUEXIN_API_KEY` in the Worker environment and use
the static connection name `yuexin`. Start Dex locally with `dexcli dev -open=false`,
then run `./gradlew modelCatalogWorker` from `java/` or `cargo run --example list_models`
from `rust/`. Start `JavaYuexinModelCatalog` with an empty JSON string or `RustYuexinModelCatalog` with `{}`.
Run `./gradlew check dexDevTest` and `cargo test --all-targets` in those directories
to replay Go scenarios and exercise retry through a real Worker and Client.
