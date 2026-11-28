# Maiyun IP location connector

The [Maiyun IP 定位 API](https://lts.maiyun.net/service/ip) accepts an IPv4 address through `POST /api/service/ip` and returns country, province, city, and district. Maiyun is operated by [柏软科技（成都）有限公司](https://lts.maiyun.net/service/ip). The connector returns only these coarse location fields and excludes the provider response and credential.

Sign in at [Maiyun LTS](https://lts.maiyun.net/), enable IP 定位, then open **控制台 > Token** to create an authorized Bearer Token. Store it in Dex Web Connections. Queries may consume provider quota. The token is sent only in the `Authorization` header.

Run the [example Flow](go/examples/lookup-ip-location/) with its generated mock or a user-controlled connection. Pass only public IPv4 addresses you are authorized to query. Provider errors, malformed data, oversized bodies, and retryable transport failures have separate outcomes. Real provider authorization and billing have not been verified locally.

The [Python lookupIPLocation example](python/examples/) provides a runnable Dex Worker and local conformance replay.

The [TypeScript lookupIPLocation example](typescript/examples/basic/) runs a Dex Worker and Client, with Go-recorded provider scenarios replayed by the package tests.

Java and Rust also implement `lookupIPLocation`. The [Java Worker](java/examples/io/superdurable/dexconnectors/maiyun/IPLocationWorker.java) and [Rust Worker](rust/examples/lookup_ip_location.rs) register the static `maiyun` connection and wire the `found` branch. Both take an object with the `ipAddress` field. Both canonicalize IPv4-mapped addresses before dispatch. Optional location fields default to empty strings.

Run these commands from this connector directory. With `dexcli dev -open=false` running, set `DEX_FLOW_SERVICE_ADDRESS` to its Dex Server address. The integration tests supply their own token and transport, return HTTP 503 once, and verify the Worker retries and completes the Flow.

```sh
(cd java && ./gradlew build dexDevTest)
(cd rust && cargo test --locked --all-targets)
dexcli visualize java/examples/io/superdurable/dexconnectors/maiyun/IPLocationWorker.java --schema-version 2.0 --lint app --json --open=false --out /tmp/maiyun-java
dexcli visualize rust/examples/lookup_ip_location.rs --schema-version 2.0 --lint app --json --open=false --out /tmp/maiyun-rust
```

The Java graph is partial because the analyzer requires the inherited `QueryStep.execute` method and the input definition in the example source. The Rust analyzer also warns about the SDK-defined `QueryStep`. The integration tests exercise both Steps through a registered Worker.

For a provider-backed Worker, supply the provider token to the process as `MAIYUN_TOKEN` and run `./gradlew ipLocationWorker` in `java/` or `cargo run --locked --example lookup_ip_location` in `rust/`. An actual lookup can consume provider quota.
