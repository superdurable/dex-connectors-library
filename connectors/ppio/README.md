# PPIO connector

This connector implements the read-only [model list API](https://ppio.com/docs/models/reference-llm-list-models). It sends a PPIO API key as a Bearer header to the fixed `https://api.ppio.com/openai/v1/models` endpoint and returns the documented model ID, display name, status, context size, supported endpoints, Unix creation time, and owner. It never places the key in Flow input, output, receipts, Attributes, or logs. An empty list is valid.

Create an API key from [API key management](https://ppio.com/settings/key-management). Enter the full key in Dex Web Connections. Rate limits and temporary service errors retry; invalid keys select `providerRejected`.

The runnable [list-models example](go/examples/list-models/README.md) demonstrates the generated operation-specific Step and static `ppio` connection. Real PPIO authorization remains to be tested with a user-controlled Dex Web Connection.

## Python

The first Python package implements the read-only operation and all Go-recorded provider branches. See [Python package](python/README.md) and its [runnable Flow](python/examples/models/README.md). Live provider authorization and responses are unverified.

The [Java Worker](java/examples/io/superdurable/dexconnectors/ppio/ModelCatalogWorker.java) and
[Rust Worker](rust/examples/list_models.rs) serve the same model-list Query.
Both examples read the key from `PPIO_API_KEY` in the Worker environment and use
the static connection name `ppio`. Start Dex locally with `dexcli dev -open=false`,
then run `./gradlew modelCatalogWorker` from `java/` or `cargo run --example list_models`
from `rust/`. Start `JavaPpioModelCatalog` with an empty JSON string or `RustPpioModelCatalog` with `{}`.
Run `./gradlew check dexDevTest` and `cargo test --all-targets` in those directories
to replay Go scenarios and exercise retry through a real Worker and Client.
