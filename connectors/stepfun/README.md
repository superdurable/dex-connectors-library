# StepFun connector

This connector implements the read-only [model list API](https://platform.stepfun.com/docs/zh/api-reference/models/list). It sends an API key as a Bearer header to the fixed `https://api.stepfun.com/v1/models` endpoint and returns the documented model ID, Unix creation time, and owner. It never places the key in Flow input, output, receipts, Attributes, or logs. An empty list is valid.

Create a personal API key from [Interface Keys](https://platform.stepfun.com/interface-key), or follow the [organization project key guide](https://platform.stepfun.com/docs/zh/guides/organization/api-keys). Enter the full key in Dex Web Connections. Rate limits and temporary service errors retry; invalid keys select `providerRejected`.

The runnable [list-models example](go/examples/list-models/README.md) demonstrates the generated operation-specific Step and static `stepfun` connection. Real StepFun authorization remains to be tested with a user-controlled Dex Web Connection.

## Python

The first Python package release is `v0.1.0`. See the [Python README](python/README.md) and its runnable Flow for the operation contract and local Dex verification.

The [TypeScript package](typescript/) provides the same `ListModels` operation, Go-recorded conformance tests, and a runnable Dex Worker and Client example.

The [Java Worker](java/examples/io/superdurable/dexconnectors/stepfun/ModelCatalogWorker.java) and
[Rust Worker](rust/examples/list_models.rs) serve the same model-list Query.
Both examples read the key from `STEPFUN_API_KEY` in the Worker environment and use
the static connection name `stepfun`. Start Dex locally with `dexcli dev -open=false`,
then run `./gradlew modelCatalogWorker` from `java/` or `cargo run --example list_models`
from `rust/`. Start `JavaStepfunModelCatalog` with an empty JSON string or `RustStepfunModelCatalog` with `{}`.
Run `./gradlew check dexDevTest` and `cargo test --all-targets` in those directories
to replay Go scenarios and exercise retry through a real Worker and Client.
