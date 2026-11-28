# 365 API connector

`ListModels` reads at most 50 model IDs and the total count from [365 API's documented GET /v1/models API](https://www.hby365.studio/developers). The service is operated by 深圳市赫比亚网络科技有限公司. A model list is scoped to the platform API Key ; listing does not request inference.

Open [365 API](https://www.hby365.studio), sign in, then create an API Key in the platform console. Copy the secret into Dex Web Connections. Blank prevents the request. Leave timeout and maxResponseBytes blank for their displayed defaults. The Query returns only model identifiers, never the Key or other provider metadata.

The checked-in [runnable model catalog Flow](go/examples/list-models/) uses `ConnectionName = "hby365"`. Run the worker with `go run .` from its directory, then start `HBY365ModelCatalog` with `{}`. Strict FDG 2.0 instructions and a Dex mock integration test are alongside the Flow.

Provider contract: [official developer documentation](https://www.hby365.studio/developers). Real provider authorization is unverified without a customer-controlled Key.

The [TypeScript package](typescript/README.md) implements `listModels` and includes a [runnable Dex Flow](typescript/examples/model-list/README.md). Its provider scenarios replay the Go-recorded contract; live provider authorization remains unverified without user-controlled credentials.

The [Python package](python/README.md) implements `listModels` with a runnable Dex Flow and Go-recorded provider conformance scenarios. Import it as `dex_connectors._365api`; the leading underscore makes the numeric directory name a valid Python module.

The [Java Worker](java/examples/io/superdurable/dexconnectors/hby365/ModelCatalogWorker.java) and
[Rust Worker](rust/examples/list_models.rs) serve the same model-list Query.
Both examples read the key from `HBY365_API_KEY` in the Worker environment and use
the static connection name `hby365`. Start Dex locally with `dexcli dev -open=false`,
then run `./gradlew modelCatalogWorker` from `java/` or `cargo run --example list_models`
from `rust/`. Start `JavaHby365ModelCatalog` with an empty JSON string or `RustHby365ModelCatalog` with `{}`.
Run `./gradlew check dexDevTest` and `cargo test --all-targets` in those directories
to replay Go scenarios and exercise retry through a real Worker and Client.
