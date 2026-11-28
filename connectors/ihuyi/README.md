# Ihuyi IP lookup connector

The [Ihuyi IP lookup API](https://www.ihuyi.com/api/ip.html) accepts a public IPv4 address and returns country, province, city, district, and network operator. The connector submits a time-bound MD5 signature to `POST https://api.ihuyi.com/appdev/ip/Submit.json` over HTTPS and filters provider messages and request IDs from the result. Ihuyi is operated by [Shanghai Sirui Information Technology](https://www.ihuyi.com/about.html).

Open [Ihuyi User Center](https://user.ihuyi.com/) > IP查询 > 产品总览 to enable the service and copy its API ID and API KEY. Store the key in Dex Web Connections. Set any required IP allowlist to include the Dex Worker. The connector never returns or logs the key, signature, request body, or raw provider error. Calls may consume the purchased query quota.

Run the [Go Flow](go/examples/lookup-ip-location/) or [Python Flow](python/examples/ip_location/) with its generated mock or a user-controlled connection. Pass only public IPv4 addresses you are authorized to query. The provider's response calls its province field `porovince`; this connector maps it to `province` in the output. A successful lookup requires a province and city; provider errors, malformed data, oversized bodies, and retryable transport failures have separate outcomes. Real provider authorization and billing have not been verified locally.

The [TypeScript package](typescript/) exposes `lookupIPLocation`, all Go-recorded provider scenarios, and a runnable Dex Worker/Client example.

Java and Rust also implement `lookupIPLocation`. The [Java Worker](java/examples/io/superdurable/dexconnectors/ihuyi/IPLocationWorker.java) and [Rust Worker](rust/examples/lookup_ip_location.rs) register the static `ihuyi` connection and wire the `found` branch. Both take an object with the `ipAddress` field. Both canonicalize IPv4-mapped addresses before dispatch. Optional location fields default to empty strings.

Run these commands from this connector directory. With `dexcli dev -open=false` running, set `DEX_FLOW_SERVICE_ADDRESS` to its Dex Server address. The integration tests supply their own token and transport, return HTTP 503 once, and verify the Worker retries and completes the Flow.

```sh
(cd java && ./gradlew build dexDevTest)
(cd rust && cargo test --locked --all-targets)
dexcli visualize java/examples/io/superdurable/dexconnectors/ihuyi/IPLocationWorker.java --schema-version 2.0 --lint app --json --open=false --out /tmp/ihuyi-java
dexcli visualize rust/examples/lookup_ip_location.rs --schema-version 2.0 --lint app --json --open=false --out /tmp/ihuyi-rust
```

The Java graph is partial because the analyzer requires the inherited `QueryStep.execute` method and the input definition in the example source. The Rust analyzer also warns about the SDK-defined `QueryStep`. The integration tests exercise both Steps through a registered Worker.

For a provider-backed Worker, supply the provider account and signing key as `IHUYI_API_ID` and `IHUYI_API_KEY` and run `./gradlew ipLocationWorker` in `java/` or `cargo run --locked --example lookup_ip_location` in `rust/`. An actual lookup can consume provider quota.
