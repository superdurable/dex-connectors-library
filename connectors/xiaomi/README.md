# Xiaomi MiMo connector

`ListModels` reads IDs from Xiaomi MiMo's official [GET /v1/models endpoint](https://mimo.mi.com/docs/zh-CN/api/model/list-models) over HTTPS with Bearer authentication. Output contains the provider model count and at most the first 50 IDs. Inference, model metadata and account data are excluded.

## Authorization

Sign in to the [MiMo API platform](https://platform.xiaomimimo.com) with a Xiaomi account, open Console > API Keys, create a pay-as-you-go API Key and copy its `sk-` secret into Dex Web Connections. The [official API integration guide](https://mimo.mi.com/docs/zh-CN/quick-start/faq/api-integration) distinguishes this Key from Token Plan `tp-` Keys, which use a separate base URL. Leave timeout and maximum response size blank for the displayed defaults.

## Example

The [runnable Flow](go/examples/list-models/) starts with `{}` and completes with a typed `ListModelsOutput`. Its default mock returns one model. Live MiMo authorization, model visibility and response variation remain unverified without a user-controlled Dex Web Connection.

## Python

The `python/` package provides the `listModels` Query. Run its checked-in example from `python/examples/`.

The [TypeScript package](typescript/README.md) implements `listModels` with a [runnable Dex Flow](typescript/examples/model-list/README.md). Its provider scenarios replay the Go-recorded contract; live authorization remains unverified without user-controlled credentials.

The [Java Worker](java/examples/io/superdurable/dexconnectors/xiaomi/ModelCatalogWorker.java) and
[Rust Worker](rust/examples/list_models.rs) serve the same model-list Query.
Both examples read the key from `XIAOMI_API_KEY` in the Worker environment and use
the static connection name `xiaomi`. Start Dex locally with `dexcli dev -open=false`,
then run `./gradlew modelCatalogWorker` from `java/` or `cargo run --example list_models`
from `rust/`. Start `JavaXiaomiModelCatalog` with an empty JSON string or `RustXiaomiModelCatalog` with `{}`.
Run `./gradlew check dexDevTest` and `cargo test --all-targets` in those directories
to replay Go scenarios and exercise retry through a real Worker and Client.
