# Juxingyi connector

`ListModels` reads at most 50 model IDs and the total count from [Juxingyi's documented GET /v1/models API](https://fireworks-simulator.huo15.com/square). The service is operated by 青岛火一五信息科技有限公司. A model list is scoped to the platform API Key and its model group; listing does not request inference.

Open [Juxingyi](https://fireworks-simulator.huo15.com), sign in, then create an API Key in the platform console. Copy the secret into Dex Web Connections. Blank prevents the request. Leave timeout and maxResponseBytes blank for their displayed defaults. The Query returns only model identifiers, never the Key or other provider metadata.

The checked-in [runnable model catalog Flow](go/examples/list-models/) uses `ConnectionName = "huo15"`. Run the worker with `go run .` from its directory, then start `Huo15ModelCatalog` with `{}`. Strict FDG 2.0 instructions and a Dex mock integration test are alongside the Flow.

Provider contract: [official developer documentation](https://fireworks-simulator.huo15.com/square). Real provider authorization is unverified without a customer-controlled Key.

The [Python package](python/) implements `listModels` and replays the Go-recorded provider scenarios. Its checked-in Flow tests retry and persistence against a real Dex Worker and Client.

The [TypeScript package](typescript/) implements the same Query and Go-recorded scenarios. Its runnable Flow tests retry and persistence against a real Dex Worker and Client.

The [Java Worker](java/examples/io/superdurable/dexconnectors/huo15/ModelCatalogWorker.java) and
[Rust Worker](rust/examples/list_models.rs) serve the same model-list Query.
Both examples read the key from `HUO15_API_KEY` in the Worker environment and use
the static connection name `huo15`. Start Dex locally with `dexcli dev -open=false`,
then run `./gradlew modelCatalogWorker` from `java/` or `cargo run --example list_models`
from `rust/`. Start `JavaHuo15ModelCatalog` with an empty JSON string or `RustHuo15ModelCatalog` with `{}`.
Run `./gradlew check dexDevTest` and `cargo test --all-targets` in those directories
to replay Go scenarios and exercise retry through a real Worker and Client.
