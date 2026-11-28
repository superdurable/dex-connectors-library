# YikeAPI IP Lookup

The [official IP location API](https://www.yikeapi.com/index/ip) accepts a public IPv4 address through HTTPS `GET /ip/` and returns country, province, city, and district. The provider identifies itself as 济南易客云网络科技有限公司 in its [contact page](https://yikeapi.com/index/contact).

Register at [YikeAPI](https://www.yikeapi.com), open 用户中心, find `appid` and `appsecret`, and add a `yikeapi` connection in Dex Web. Activate the IP address service and check account quota. Both credentials are secrets required in the provider's HTTPS query, so avoid logging request URLs or request objects. The connector excludes the credentials and raw response from Flow output and errors. Leave timeout and maxResponseBytes blank for their displayed defaults.

The [runnable Dex example](go/examples/lookup-ip-location/) uses the static `yikeapi` connection. Its default mock is synthetic and does not verify live provider authorization or response data.

## Python

The `python/` package provides the `lookupIPLocation` Query. Run its checked-in example from `python/examples/`.

The [TypeScript package](typescript/README.md) implements `lookupIPLocation` with a [runnable Dex Flow](typescript/examples/basic/README.md). Its provider scenarios replay the Go-recorded contract; live authorization remains unverified without user-controlled credentials.

Java and Rust also implement `lookupIPLocation`. The [Java Worker](java/examples/io/superdurable/dexconnectors/yikeapi/IPLocationWorker.java) and [Rust Worker](rust/examples/lookup_ip_location.rs) register the static `yikeapi` connection and wire the `found` branch. Both take an object with the `ipAddress` field. Both canonicalize IPv4-mapped addresses before dispatch. Optional location fields default to empty strings.

Run these commands from this connector directory. With `dexcli dev -open=false` running, set `DEX_FLOW_SERVICE_ADDRESS` to its Dex Server address. The integration tests supply their own token and transport, return HTTP 503 once, and verify the Worker retries and completes the Flow.

```sh
(cd java && ./gradlew build dexDevTest)
(cd rust && cargo test --locked --all-targets)
dexcli visualize java/examples/io/superdurable/dexconnectors/yikeapi/IPLocationWorker.java --schema-version 2.0 --lint app --json --open=false --out /tmp/yikeapi-java
dexcli visualize rust/examples/lookup_ip_location.rs --schema-version 2.0 --lint app --json --open=false --out /tmp/yikeapi-rust
```

The Java graph is partial because the analyzer requires the inherited `QueryStep.execute` method and the input definition in the example source. The Rust analyzer also warns about the SDK-defined `QueryStep`. The integration tests exercise both Steps through a registered Worker.

For a provider-backed Worker, supply the application ID and secret as `YIKEAPI_APP_ID` and `YIKEAPI_APP_SECRET` and run `./gradlew ipLocationWorker` in `java/` or `cargo run --locked --example lookup_ip_location` in `rust/`. An actual lookup can consume provider quota.
