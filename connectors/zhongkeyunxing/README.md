# Zhongke Yunxing connector

`ListModels` reads at most 50 model IDs and the total count from [Zhongke Yunxing's documented GET /v1/models API](https://www.zhongkeyunx.com/brand/guide.html). The service is operated by 大连中科云星技术服务有限公司. A model list is scoped to the platform API Key and its model group; listing does not request inference.

Open [Zhongke Yunxing](https://www.zhongkeyunx.com), sign in, then create an API Key in the platform console. Copy the secret into Dex Web Connections. Blank prevents the request. Leave timeout and maxResponseBytes blank for their displayed defaults. The Query returns only model identifiers, never the Key or other provider metadata.

The checked-in [runnable model catalog Flow](go/examples/list-models/) uses `ConnectionName = "zhongkeyunxing"`. Run the worker with `go run .` from its directory, then start `ZhongkeYunxingModelCatalog` with `{}`. Strict FDG 2.0 instructions and a Dex mock integration test are alongside the Flow.

Provider contract: [official developer documentation](https://www.zhongkeyunx.com/brand/guide.html). Real provider authorization is unverified without a customer-controlled Key.

## Python

The `python/` package provides the `listModels` Query. Run its checked-in example from `python/examples/`.

The [TypeScript package](typescript/README.md) implements `listModels` with a [runnable Dex Flow](typescript/examples/model-list/README.md). Its provider scenarios replay the Go-recorded contract; live authorization remains unverified without user-controlled credentials.

The [Java Worker](java/examples/io/superdurable/dexconnectors/zhongkeyunxing/ModelCatalogWorker.java) and
[Rust Worker](rust/examples/list_models.rs) serve the same model-list Query.
Both examples read the key from `ZHONGKEYUNXING_API_KEY` in the Worker environment and use
the static connection name `zhongkeyunxing`. Start Dex locally with `dexcli dev -open=false`,
then run `./gradlew modelCatalogWorker` from `java/` or `cargo run --example list_models`
from `rust/`. Start `JavaZhongkeyunxingModelCatalog` with an empty JSON string or `RustZhongkeyunxingModelCatalog` with `{}`.
Run `./gradlew check dexDevTest` and `cargo test --all-targets` in those directories
to replay Go scenarios and exercise retry through a real Worker and Client.
