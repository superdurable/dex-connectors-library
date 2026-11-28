# Shuntianchuang connector

`ListModels` reads at most 50 model IDs and the total count from [Shuntianchuang's documented GET /v1/models API](https://docs.shuntianchuang.cn/tokenhub/api/base.html). The service is operated by 深圳顺天创科技有限公司. A model list is scoped to the platform API Key and its model group; listing does not request inference.

Open [Shuntianchuang](https://api-model.shuntianchuang.cn), sign in, then create an API Key in the platform console. Copy the secret into Dex Web Connections. Blank prevents the request. Leave timeout and maxResponseBytes blank for their displayed defaults. The Query returns only model identifiers, never the Key or other provider metadata.

The checked-in [runnable model catalog Flow](go/examples/list-models/) uses `ConnectionName = "shuntianchuang"`. Run the worker with `go run .` from its directory, then start `ShuntianchuangModelCatalog` with `{}`. Strict FDG 2.0 instructions and a Dex mock integration test are alongside the Flow.

Provider contract: [official developer documentation](https://docs.shuntianchuang.cn/tokenhub/api/base.html). Real provider authorization is unverified without a customer-controlled Key.

## Python

The `python/` package provides the `listModels` Query. Run its checked-in example from `python/examples/`.
## TypeScript

`@superdurable/connector-shuntianchuang` provides the `listModels` Query and a [runnable TypeScript Dex example](typescript/examples/basic/README.md). The package replays Go-recorded provider scenarios with `npm run check`, and `npm run test:integration` verifies retry and persistence against `dexcli dev -open=false`. Real provider credentials and live API behavior have not been verified.

The [Java Worker](java/examples/io/superdurable/dexconnectors/shuntianchuang/ModelCatalogWorker.java) and
[Rust Worker](rust/examples/list_models.rs) serve the same model-list Query.
Both examples read the key from `SHUNTIANCHUANG_API_KEY` in the Worker environment and use
the static connection name `shuntianchuang`. Start Dex locally with `dexcli dev -open=false`,
then run `./gradlew modelCatalogWorker` from `java/` or `cargo run --example list_models`
from `rust/`. Start `JavaShuntianchuangModelCatalog` with an empty JSON string or `RustShuntianchuangModelCatalog` with `{}`.
Run `./gradlew check dexDevTest` and `cargo test --all-targets` in those directories
to replay Go scenarios and exercise retry through a real Worker and Client.
