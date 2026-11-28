# ShinLinkAI connector

`ListModels` reads the first 50 model IDs and total count from [ShinLinkAI's GET /v1/models API](https://api.shinloo.com/docs/). The [official site](https://api.shinloo.com/) identifies 上海玺芮实业有限公司 as the operator. The documented production base URL is `https://api.shinloo.com/v1`; Keys use the `sk-sl-` prefix.

Register at [the console](https://console.shinloo.com), create an API Key and copy the secret once into Dex Web Connections. A blank Key blocks the request. Leave timeout and maxResponseBytes blank for their displayed defaults. The Query does not invoke inference or return billing data.

The checked-in [runnable model catalog Flow](go/examples/list-models/) uses `ConnectionName = "shinloo"`. Run the worker with `go run .` from its directory, then start `ShinLinkAIModelCatalog` with `{}`. Strict FDG 2.0 instructions and a Dex mock integration test are alongside the Flow.

Provider contract: [API documentation](https://api.shinloo.com/docs/). Real provider authorization is not verified without a customer-controlled Key.

## Python

The first Python package release is `v0.1.0`. See the [Python README](python/README.md) and its runnable Flow for the operation contract and local Dex verification.

The [TypeScript package](typescript/) provides the same `ListModels` operation, Go-recorded conformance tests, and a runnable Dex Worker and Client example.

The [Java Worker](java/examples/io/superdurable/dexconnectors/shinloo/ModelCatalogWorker.java) and
[Rust Worker](rust/examples/list_models.rs) serve the same model-list Query.
Both examples read the key from `SHINLOO_API_KEY` in the Worker environment and use
the static connection name `shinloo`. Start Dex locally with `dexcli dev -open=false`,
then run `./gradlew modelCatalogWorker` from `java/` or `cargo run --example list_models`
from `rust/`. Start `JavaShinlooModelCatalog` with an empty JSON string or `RustShinlooModelCatalog` with `{}`.
Run `./gradlew check dexDevTest` and `cargo test --all-targets` in those directories
to replay Go scenarios and exercise retry through a real Worker and Client.
