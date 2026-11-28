# SenseTime SenseNova connector

`ListModels` reads model IDs from SenseTime SenseNova's official [GET /v1/llm/models endpoint](https://console.sensecore.cn/micro/help/docs/model-as-a-service/nova/overview/Models/GetModelList/) over HTTPS with Bearer API Key authentication. Output contains the provider model count and at most the first 50 IDs. Inference, model permissions and account data are excluded.

## Authorization

Sign in to the [SenseCore console](https://console.sensecore.cn), activate SenseNova, then open ModelStudio Management Center > API Key Management and create a Key. Copy its secret into Dex Web Connections. The [official authentication guide](https://console.sensecore.cn/micro/help/docs/model-as-a-service/nova/overview/Authorization/) says a directly supplied API Key may replace a generated JWT in the Bearer header. Leave timeout and maximum response size blank for the displayed defaults.

## Example

The [runnable Flow](go/examples/list-models/) starts with `{}` and completes with a typed `ListModelsOutput`. Its default mock returns one model. Live SenseNova authorization, model visibility and provider response variation remain unverified without a user-controlled Dex Web Connection.

## Python

The first Python package release is `v0.1.0`. See the [Python README](python/README.md) and its runnable Flow for the operation contract and local Dex verification.

The [TypeScript package](typescript/) provides the same `ListModels` operation, Go-recorded conformance tests, and a runnable Dex Worker and Client example.

The [Java Worker](java/examples/io/superdurable/dexconnectors/sensetime/ModelCatalogWorker.java) and
[Rust Worker](rust/examples/list_models.rs) serve the same model-list Query.
Both examples read the key from `SENSETIME_API_KEY` in the Worker environment and use
the static connection name `sensetime`. Start Dex locally with `dexcli dev -open=false`,
then run `./gradlew modelCatalogWorker` from `java/` or `cargo run --example list_models`
from `rust/`. Start `JavaSensetimeModelCatalog` with an empty JSON string or `RustSensetimeModelCatalog` with `{}`.
Run `./gradlew check dexDevTest` and `cargo test --all-targets` in those directories
to replay Go scenarios and exercise retry through a real Worker and Client.
