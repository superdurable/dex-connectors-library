# Rainbond connector

Rainbond is operated by [北京好雨科技有限公司](https://www.goodrain.com/service/). Its [v5.12 OpenAPI guide](https://v5.12-docs.rainbond.com/docs/intro/) and [team-list contract](https://v5.12-docs.rainbond.com/docs/api/team/getteamlist/) document `GET /openapi/v1/teams`, raw access-token authorization, and the team response. This connector reads one bounded page and outputs team ID, name, active flag, and total count. It excludes creator names, roles, and other personal fields.

## Connection

Open your organization's Rainbond console over HTTPS. In the top-right menu choose **个人中心 > 访问令牌 > 新增**, create a token, and enter it in Dex as `api_key`. Use an account whose team access is limited to what the Flow needs. Enter only the console HTTPS origin as `baseUrl`; the connector adds `/openapi/v1/teams`. The connector rejects non-HTTPS public origins. Timeout and response limit have defaults in Dex Web. The connector targets the documented v5.12 OpenAPI, so check compatibility with other Rainbond versions before using them.

`listTeams` accepts one-based `page` and `pageSize` fields. Zero values use page 1 and 20 items. The connector caps page size at 100, selects `listed` for a valid team page, and safely retries transient failures.

## Example

The [team catalog Flow](go/examples/team-catalog/) is a runnable Worker with static Dex connection name `rainbond`. Its local mock uses synthetic team data and can be verified through a real Dex Worker and Client. Live console authorization, version compatibility, and provider pagination remain unverified until a user connects an approved Rainbond deployment.

The [Python package](python/) implements `listTeams` and its [runnable Flow](python/examples/current_query/) verifies retry and persistence with a real local Dex Worker and Client.
The [TypeScript package](typescript/) provides the same `ListTeams` operation, Go-recorded conformance, and a runnable Dex Worker and Client example.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/rainbond/TeamDirectoryWorker.java) registers `JavaRainbondTeamDirectory`. Run `./gradlew teamDirectoryWorker` from `java/` with `RAINBOND_API_KEY` supplied through your credential manager. Set `RAINBOND_BASE_URL` to the configured provider origin. Hosted Java applications can use `TeamDirectory.fromProject` with their loaded connection.

The [Rust Worker](rust/examples/list_teams.rs) registers `RustRainbondTeamDirectory`. From `rust/`, run `cargo run --example list_teams` with `DEX_CONNECTOR_CONFIG_FILE` pointing at the connections file exported by Dex Web. Both examples use the static `rainbond` connection name and `DEX_FLOW_SERVICE_ADDRESS` to select the Dex server. Supply `page` and `pageSize`. Zero selects page one and twenty teams.

Generate and lint the Flow Definition Graphs from the connector directory.

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/rainbond/TeamDirectoryWorker.java --language java --schema-version 2.0 --lint app --open=false --json --out /tmp/rainbond-java
dexcli visualize rust/examples/list_teams.rs --language rust --schema-version 2.0 --lint app --open=false --json --out /tmp/rainbond-rust
```
