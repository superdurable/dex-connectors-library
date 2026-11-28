# NavInfo LBS connector

`LookupIPLocation` calls the official [IP定位 REST API](https://lbs.navinfo.com/docs/webapi/ipcity.html) over HTTPS and returns only province, city, and administrative code for one IP address. It does not return or store the provider's full response. The [NavInfo platform](https://lbs.navinfo.com/docs/webapi/summary.html) is operated by 北京四维图新科技股份有限公司.

Open the [IP定位 API documentation](https://lbs.navinfo.com/docs/webapi/ipcity.html), click 申请 beside the AK parameter, sign in, and obtain a REST API AK with IP定位 access. Copy this secret into Dex Web Connections. The AK is sent in the HTTPS query string; redirects are disabled and errors omit the request URL and AK. Leave timeout and maxResponseBytes blank for displayed defaults.

Start the [runnable Flow](go/examples/lookup-ip-location/) with `{"ipAddress":"8.8.8.8"}`. Its default mock returns a Beijing example. Live AK authorization and location results have not been verified with a provider account.

The [Python package](python/README.md) implements the same Query with a checked-in Dex Worker and Client example. Its Go-recorded provider scenarios and local Dex retry path are verified; live provider authorization remains unverified.
The [TypeScript package](typescript/README.md) exposes `lookupIPLocation` and includes a runnable Dex Flow, Go scenario replay, and a local provider integration test.

Java and Rust also implement `lookupIPLocation`. The [Java Worker](java/examples/io/superdurable/dexconnectors/navinfo/IPLocationWorker.java) and [Rust Worker](rust/examples/lookup_ip_location.rs) register the static `navinfo` connection and wire the `found` branch. Both take an object with the `ipAddress` field. NavInfo accepts IPv4 literals and IPv4-mapped IPv6. It rejects responses that repeat the connection key.

Run these commands from this connector directory. With `dexcli dev -open=false` running, set `DEX_FLOW_SERVICE_ADDRESS` to its Dex Server address. The integration tests supply their own token and transport, return HTTP 503 once, and verify the Worker retries and completes the Flow.

```sh
(cd java && ./gradlew build dexDevTest)
(cd rust && cargo test --locked --all-targets)
dexcli visualize java/examples/io/superdurable/dexconnectors/navinfo/IPLocationWorker.java --schema-version 2.0 --lint app --json --open=false --out /tmp/navinfo-java
dexcli visualize rust/examples/lookup_ip_location.rs --schema-version 2.0 --lint app --json --open=false --out /tmp/navinfo-rust
```

The Java graph is partial because the analyzer requires the inherited `QueryStep.execute` method and the input definition in the example source. The Rust analyzer also warns about the SDK-defined `QueryStep`. The integration tests exercise both Steps through a registered Worker.

For a provider-backed Worker, supply the provider API key to the process as `NAVINFO_AK` and run `./gradlew ipLocationWorker` in `java/` or `cargo run --locked --example lookup_ip_location` in `rust/`. An actual lookup can consume provider quota.
