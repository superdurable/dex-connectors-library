# IPDataCloud IP lookup connector

The [IPDataCloud v2 query API](https://help.ipdatacloud.com/doc/ipguishudi/?tp=3&type=2) accepts an IP address and returns a `data.location` object. This connector queries public IPv4 addresses and maps country, province, city, district, and ISP to a bounded output. IPDataCloud is operated by [江苏舍恩伯格信息科技有限公司](https://help.ipdatacloud.com/doc/ipguishudi/?tp=3&type=2).

Sign in at the [IPDataCloud center](https://myip.ipdatacloud.com/), subscribe to the global IP location API, then open 个人中心 > API管理 to find its product KEY. Store it in Dex Web Connections. The provider's documented HTTPS request puts the KEY in a query parameter; the connector excludes it and raw provider messages from results and errors. Calls may consume purchased query quota.

Run the [example Flow](go/examples/lookup-ip-location/) with its generated mock or a user-controlled connection. Pass only public IPv4 addresses you are authorized to query. Provider errors, malformed data, oversized bodies, and retryable transport failures have separate outcomes. Real provider authorization and billing have not been verified locally.

## Python

The first Python package lives in [`python/`](python/). It implements `lookupIPLocation` with Go-equivalent branches and retries. Its runnable [`IP location Flow`](python/examples/ip_location/) uses a static `ipdatacloud` connection and a real Dex Worker/Client test with a mock provider. Live provider authorization has not been verified.

## TypeScript

The [TypeScript package](typescript/) provides the same Query with a runnable Dex Worker/Client example and Go-recorded conformance replay.

Java and Rust also implement `lookupIPLocation`. The [Java Worker](java/examples/io/superdurable/dexconnectors/ipdatacloud/IPLocationWorker.java) and [Rust Worker](rust/examples/lookup_ip_location.rs) register the static `ipdatacloud` connection and wire the `found` branch. Both take an object with the `ipAddress` field. Both canonicalize IPv4-mapped addresses before dispatch. Optional location fields default to empty strings.

Run these commands from this connector directory. With `dexcli dev -open=false` running, set `DEX_FLOW_SERVICE_ADDRESS` to its Dex Server address. The integration tests supply their own token and transport, return HTTP 503 once, and verify the Worker retries and completes the Flow.

```sh
(cd java && ./gradlew build dexDevTest)
(cd rust && cargo test --locked --all-targets)
dexcli visualize java/examples/io/superdurable/dexconnectors/ipdatacloud/IPLocationWorker.java --schema-version 2.0 --lint app --json --open=false --out /tmp/ipdatacloud-java
dexcli visualize rust/examples/lookup_ip_location.rs --schema-version 2.0 --lint app --json --open=false --out /tmp/ipdatacloud-rust
```

The Java graph is partial because the analyzer requires the inherited `QueryStep.execute` method and the input definition in the example source. The Rust analyzer also warns about the SDK-defined `QueryStep`. The integration tests exercise both Steps through a registered Worker.

For a provider-backed Worker, supply the provider API key to the process as `IPDATACLOUD_API_KEY` and run `./gradlew ipLocationWorker` in `java/` or `cargo run --locked --example lookup_ip_location` in `rust/`. An actual lookup can consume provider quota.
