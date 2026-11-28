# IPPlus360 IP lookup connector

The [IPPlus360 district-level IPv4 API](https://mall.ipplus360.com/pros/IPVFourGeoAPI) maps a public IPv4 address to country, province, city, district, and ISP. This connector uses the documented HTTPS endpoint `GET /ip/geo/v1/district/` and returns only those coarse fields. IPPlus360 is operated by 郑州埃文科技有限公司.

Open the [IPPlus360 product page](https://mall.ipplus360.com/pros/IPVFourGeoAPI), activate the **区县级** IPv4 product, then sign in at [个人中心 > 我的订单](https://account.ipplus360.com/setting) to copy that product's KEY. Store it in Dex Web Connections. The provider's documented request puts the KEY in a query parameter; the connector omits it and raw provider messages from results and errors. Successful lookups may consume purchased quota.

Run the [example Flow](go/examples/lookup-ip-location/) with its generated mock or a user-controlled connection. Pass only public IPv4 addresses you are authorized to query. Provider errors, malformed data, oversized bodies, and retryable transport failures have separate outcomes. Real provider authorization and billing have not been verified locally.

## Python

The first Python package lives in [`python/`](python/). It implements `lookupIPLocation` with Go-equivalent branches and retries. Its runnable [`IP location Flow`](python/examples/ip_location/) uses a static `ipplus360` connection and a real Dex Worker/Client test with a mock provider. Live provider authorization has not been verified.

## TypeScript

The [TypeScript package](typescript/) provides the same Query with a runnable Dex Worker/Client example and Go-recorded conformance replay.

Java and Rust also implement `lookupIPLocation`. The [Java Worker](java/examples/io/superdurable/dexconnectors/ipplus360/IPLocationWorker.java) and [Rust Worker](rust/examples/lookup_ip_location.rs) register the static `ipplus360` connection and wire the `found` branch. Both take an object with the `ipAddress` field. Both canonicalize IPv4-mapped addresses before dispatch. Optional location fields default to empty strings.

Run these commands from this connector directory. With `dexcli dev -open=false` running, set `DEX_FLOW_SERVICE_ADDRESS` to its Dex Server address. The integration tests supply their own token and transport, return HTTP 503 once, and verify the Worker retries and completes the Flow.

```sh
(cd java && ./gradlew build dexDevTest)
(cd rust && cargo test --locked --all-targets)
dexcli visualize java/examples/io/superdurable/dexconnectors/ipplus360/IPLocationWorker.java --schema-version 2.0 --lint app --json --open=false --out /tmp/ipplus360-java
dexcli visualize rust/examples/lookup_ip_location.rs --schema-version 2.0 --lint app --json --open=false --out /tmp/ipplus360-rust
```

The Java graph is partial because the analyzer requires the inherited `QueryStep.execute` method and the input definition in the example source. The Rust analyzer also warns about the SDK-defined `QueryStep`. The integration tests exercise both Steps through a registered Worker.

For a provider-backed Worker, supply the provider API key to the process as `IPPLUS360_API_KEY` and run `./gradlew ipLocationWorker` in `java/` or `cargo run --locked --example lookup_ip_location` in `rust/`. An actual lookup can consume provider quota.
