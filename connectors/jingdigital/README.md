# JINGdigital connector

JINGdigital is operated by 上海径硕网络科技有限公司. This connector reads one page of ordinary marketing tag IDs, names, and hierarchy levels. It does not return users, member profiles, or access tokens.

The [official access guide](https://s.apifox.cn/739a1394-cf66-49a7-ba16-c06ffa7a6399/doc-1781384) describes credential setup. The [token endpoint](https://s.apifox.cn/739a1394-cf66-49a7-ba16-c06ffa7a6399/api-52635744) exchanges `app_id` and `app_secret`; the [tag list endpoint](https://s.apifox.cn/739a1394-cf66-49a7-ba16-c06ffa7a6399/api-141832893) reads a page using `app_id` and the temporary `access_token` headers. The [global error codes](https://s.apifox.cn/739a1394-cf66-49a7-ba16-c06ffa7a6399/doc-1781394) distinguish rejection, unavailable service, and rate limits. The documented default limit is 50 calls per second and 100,000 daily calls per tenant per interface.

## Connection

In the JINGdigital management backend, open **开发平台 > 开发与集成**, create or select a development application, enable ordinary tag list access, and copy its Appid and AppSecret into the secret connection fields. Leave endpoint and limits blank for defaults. The connector requests a temporary token on each Query; neither token nor secret enters Flow state. Provider 429, 408, 5xx, transport failures, and documented service/rate codes retry. Other rejections take an optional branch.

Run the [Go tag directory example](go/examples/tag-directory/README.md) or the [Python tag directory example](python/examples/tag_directory/README.md). Its synthetic mock is not live JINGdigital data. Live credentials, permissions, and response compatibility remain unverified.

Missing tag levels and missing total counts are returned as zero.

## Java and Rust

The `listTags` Step exchanges the application credentials for an access token on every attempt. Start with `{"page":1,"pageSize":20}`. Zero selects the first page and the default page size of 20. A positive `nextPage` identifies the next page to request.

The [Java Worker](java/examples/io/superdurable/dexconnectors/jingdigital/TagDirectoryWorker.java) serves `JavaJINGdigitalTagDirectory`. From `java/`, run `./gradlew tagDirectoryWorker` with `JINGDIGITAL_APP_ID` and `JINGDIGITAL_APP_SECRET` supplied by your credential manager. Hosted Java applications can bind their loaded Dex project with `TagDirectory.fromProject(project, "jingdigital")`.

The [Rust Worker](rust/examples/list_tags.rs) serves `RustJINGdigitalTagDirectory`. Set `DEX_CONNECTOR_CONFIG_FILE` to the connections file exported by Dex Web with the named `jingdigital` connection. From `rust/`, run `cargo run --example list_tags`. Both examples use `DEX_FLOW_SERVICE_ADDRESS` to select the Dex server.

Generate the Flow Definition Graphs from the connector directory.

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/jingdigital/TagDirectoryWorker.java --language java --schema-version 2.0 --lint app --open=false --json --out /tmp/jingdigital-java
dexcli visualize rust/examples/list_tags.rs --language rust --schema-version 2.0 --lint app --open=false --json --out /tmp/jingdigital-rust
```
