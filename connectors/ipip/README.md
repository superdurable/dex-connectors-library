# IPIP connector

This connector calls IPIP's official [v2 online IP query API](https://www.ipip.net/document/api_v2.html), `GET https://ipapi.ipip.net/v2/query/{IP}?token=...`, and returns only the default country, region, city, and two-letter country-code fields. It ignores all custom-only fields, coordinates, domains, and provider messages. IPIP is operated by [Beijing Tiantexin Technology Co., Ltd.](https://www.ipip.net/contact.html).

Sign in or register at [IPIP](https://user.ipip.net/) and obtain a token for your online query service. The public API document does not identify the exact account page where the token appears; [IPIP sales](https://www.ipip.net/contact.html) can provide account-specific instructions. Save the token in Dex Web Connections. The provider requires it in the HTTPS query string, so the connector disables redirects and never returns or logs the URL, token, or raw error message.

Run the [example Flow](go/examples/lookup-ip-location/) with its generated mock or a user-controlled connection. An IP address can identify a device or person; pass only addresses you are authorized to query. Live pricing and quota depend on your IPIP agreement and are not stated in the v2 API document.

## Python

The first Python package lives in [`python/`](python/). It implements `lookupIPLocation` with the same branches and retry decisions as Go. Its runnable [`IP location Flow`](python/examples/ip_location/) uses the static `ipip` connection and includes a real Dex Worker/Client integration test with a mock IPIP HTTP response. Live provider authorization has not been verified.

## TypeScript

The [TypeScript package](typescript/) adds the same lookup Query and a runnable Dex Worker/Client example. It replays the Go-recorded provider scenarios before release.

Java and Rust also implement `lookupIPLocation`. The [Java Worker](java/examples/io/superdurable/dexconnectors/ipip/IPLocationWorker.java) and [Rust Worker](rust/examples/lookup_ip_location.rs) register the static `ipip` connection and wire the `found` branch. Both take an object with the `ipAddress` field. IPIP accepts IPv4 and IPv6 literals.

Run these commands from this connector directory. With `dexcli dev -open=false` running, set `DEX_FLOW_SERVICE_ADDRESS` to its Dex Server address. The integration tests supply their own token and transport, return HTTP 503 once, and verify the Worker retries and completes the Flow.

```sh
(cd java && ./gradlew build dexDevTest)
(cd rust && cargo test --locked --all-targets)
dexcli visualize java/examples/io/superdurable/dexconnectors/ipip/IPLocationWorker.java --schema-version 2.0 --lint app --json --open=false --out /tmp/ipip-java
dexcli visualize rust/examples/lookup_ip_location.rs --schema-version 2.0 --lint app --json --open=false --out /tmp/ipip-rust
```

The Java graph is partial because the analyzer requires the inherited `QueryStep.execute` method and the input definition in the example source. The Rust analyzer also warns about the SDK-defined `QueryStep`. The integration tests exercise both Steps through a registered Worker.

For a provider-backed Worker, supply the provider API key to the process as `IPIP_TOKEN` and run `./gradlew ipLocationWorker` in `java/` or `cargo run --locked --example lookup_ip_location` in `rust/`. An actual lookup can consume provider quota.
