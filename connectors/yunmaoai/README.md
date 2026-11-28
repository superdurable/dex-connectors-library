# YunMao AI connector

`ListModels` reads IDs visible to a 云猫 API Key. The [official developer guide](https://yunmaoai.com/docs.html) documents `GET https://api.yunmaoai.com/v1/models`, Bearer authentication, and OpenAI compatible API behavior. Output contains the provider model count and at most the first 50 IDs. The provider says the list may include models that have not been verified callable; inference and model metadata are excluded.

## Authorization

Start at the [云猫 registration page](https://api.yunmaoai.com/sign-up), then open the console's 令牌 or API 密钥 page, create a Key, and copy its secret into Dex Web Connections. The [official guide](https://yunmaoai.com/docs.html) states the Bearer header format. Leave timeout and maximum response size blank for the displayed defaults.

## Example

The [runnable Flow](go/examples/list-models/) starts with `{}` and completes with a typed `ListModelsOutput`. Its default mock returns one model. Live 云猫 authorization, model visibility and response variation remain unverified without a user-controlled Dex Web Connection.

## Python

The `python/` package provides the `listModels` Query. Run its checked-in example from `python/examples/`.

The [TypeScript package](typescript/README.md) implements `listModels` with a [runnable Dex Flow](typescript/examples/model-list/README.md). Its provider scenarios replay the Go-recorded contract; live authorization remains unverified without user-controlled credentials.

The [Java Worker](java/examples/io/superdurable/dexconnectors/yunmaoai/ModelCatalogWorker.java) and
[Rust Worker](rust/examples/list_models.rs) serve the same model-list Query.
Both examples read the key from `YUNMAOAI_API_KEY` in the Worker environment and use
the static connection name `yunmaoai`. Start Dex locally with `dexcli dev -open=false`,
then run `./gradlew modelCatalogWorker` from `java/` or `cargo run --example list_models`
from `rust/`. Start `JavaYunmaoaiModelCatalog` with an empty JSON string or `RustYunmaoaiModelCatalog` with `{}`.
Run `./gradlew check dexDevTest` and `cargo test --all-targets` in those directories
to replay Go scenarios and exercise retry through a real Worker and Client.
