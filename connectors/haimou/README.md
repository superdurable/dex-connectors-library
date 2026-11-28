# Haimou IP lookup connector

The [亥牟数据 IP 归属地 API](https://hmkj4399.com/detail-ip.html) accepts a public IPv4 address through `GET /v1/ip` and returns country, province, city, district, and ISP. The service is operated by [杭州亥牟科技有限公司](https://hmkj4399.com/index.html). This connector returns only coarse location and operator fields and excludes the provider response and credential.

Sign in at [亥牟数据](https://hmkj4399.com/), complete provider account verification, then open **控制台 > 应用管理** and create an application authorized for IP 归属地. Copy its secret apikey into Dex Web Connections. The provider requires this key in the HTTPS query string, so avoid logging request URLs. Each query may consume provider quota; reset a disclosed key in application details.

Run the [example Flow](go/examples/lookup-ip-location/) with its generated mock or a user-controlled connection. Pass only public IPv4 addresses you are authorized to query. Provider errors, malformed data, oversized bodies, and retryable transport failures have separate outcomes. Real provider authorization and billing have not been verified locally.

The [TypeScript package](typescript/) has the same `lookupIPLocation` Query and a [runnable Dex Worker/Client example](typescript/examples/basic/). It replays the Go-recorded provider cases without storing the API key in Flow state.

The [Python package](python/) implements the same `lookupIPLocation` Query and replays the Go-recorded provider scenarios. Its checked-in Flow tests retry and persistence against a real Dex Worker and Client.

Java and Rust also implement `lookupIPLocation`. The [Java Worker](java/examples/io/superdurable/dexconnectors/haimou/IPLocationWorker.java) and [Rust Worker](rust/examples/lookup_ip_location.rs) register the static `haimou` connection and wire the `found` branch. Both take an object with the `ipAddress` field. Both canonicalize IPv4-mapped addresses before dispatch. Optional location fields default to empty strings.

Run these commands from this connector directory. With `dexcli dev -open=false` running, set `DEX_FLOW_SERVICE_ADDRESS` to its Dex Server address. The integration tests supply their own token and transport, return HTTP 503 once, and verify the Worker retries and completes the Flow.

```sh
(cd java && ./gradlew build dexDevTest)
(cd rust && cargo test --locked --all-targets)
dexcli visualize java/examples/io/superdurable/dexconnectors/haimou/IPLocationWorker.java --schema-version 2.0 --lint app --json --open=false --out /tmp/haimou-java
dexcli visualize rust/examples/lookup_ip_location.rs --schema-version 2.0 --lint app --json --open=false --out /tmp/haimou-rust
```

The Java graph is partial because the analyzer requires the inherited `QueryStep.execute` method and the input definition in the example source. The Rust analyzer also warns about the SDK-defined `QueryStep`. The integration tests exercise both Steps through a registered Worker.

For a provider-backed Worker, supply the provider API key to the process as `HAIMOU_API_KEY` and run `./gradlew ipLocationWorker` in `java/` or `cargo run --locked --example lookup_ip_location` in `rust/`. An actual lookup can consume provider quota.
