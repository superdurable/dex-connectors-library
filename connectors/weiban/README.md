# Weiban Assistant customer tags

`ListTags` reads the first 10 customer tag definitions and the total count. It returns only tag IDs, names and group names, without customers, staff or department assignments. The [official tag-list API](https://openapi.weibanzhushou.com/api-9075864) defines `GET /open-api/tag/list?limit=10&offset=0&access_token=...`, response fields and errors. The [official token API](https://openapi.weibanzhushou.com/api-8233537) uses a JSON POST with corp_id and secret and returns a short-lived token. The [setup guide](https://openapi.weibanzhushou.com/doc-370558) identifies the administration page and basic-seat requirement. Weiban is a [MingLue product](https://www.mininglamp.com/weibanzhushou/).

The connector obtains a fresh token for each operation call, then fetches one tag page. It does not store or expose the token. The token appears in the provider-required HTTPS query parameter; redirect following is disabled and URL-bearing errors are replaced with fixed messages. HTTP 429, 408 and 5xx, transport failures, and documented busy codes retry. Other provider rejections take optional branches. Each response is bounded to 256 KiB.

The [runnable Dex Flow](go/examples/tags/README.md) demonstrates a static connection, typed result, strict FDG, and mock-backed real Dex integration. Live authorization and production responses remain unverified. The logo uses an [official Weiban page asset](https://assets.weibanzhushou.com/web/we-work-webapp/weiban-logo_logo-no-word-3.45d3d34536da.png).

## Python

The [`python/`](python/) package implements the `listTags` Query. Its checked-in example runs a Worker and a real Dex retry test; provider authorization remains unverified.

## TypeScript

The TypeScript package under [`typescript/`](typescript/) implements `listTags` with a runnable Dex Flow, Go-recorded conformance replay, and a real Dex integration test.

The connector returns the first ten tags. Missing names or groups are returned as empty strings.

## Java and Rust

The `listTags` Step exchanges the application credentials for an access token on every attempt. Start with `{}` to request the first ten tags and the total count.

The [Java Worker](java/examples/io/superdurable/dexconnectors/weiban/TagDirectoryWorker.java) serves `JavaWeibanTagDirectory`. From `java/`, run `./gradlew tagDirectoryWorker` with `WEIBAN_CORP_ID` and `WEIBAN_SECRET` supplied by your credential manager. Hosted Java applications can bind their loaded Dex project with `TagDirectory.fromProject(project, "weiban")`.

The [Rust Worker](rust/examples/list_tags.rs) serves `RustWeibanTagDirectory`. Set `DEX_CONNECTOR_CONFIG_FILE` to the connections file exported by Dex Web with the named `weiban` connection. From `rust/`, run `cargo run --example list_tags`. Both examples use `DEX_FLOW_SERVICE_ADDRESS` to select the Dex server.

Generate the Flow Definition Graphs from the connector directory.

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/weiban/TagDirectoryWorker.java --language java --schema-version 2.0 --lint app --open=false --json --out /tmp/weiban-java
dexcli visualize rust/examples/list_tags.rs --language rust --schema-version 2.0 --lint app --open=false --json --out /tmp/weiban-rust
```
