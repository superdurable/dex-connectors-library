# ONES connector

[ONES](https://ones.cn/aboutus) is operated by 深圳复临科技有限公司. This Go connector calls the [ONES Open API v2 team-list endpoint](https://docs.ones.cn/developer/openapi/api/01-get-organization-teams/) to list teams accessible to the connected user. It returns only team UUIDs, names, and numeric statuses, omitting owner identifiers and timestamps.

## Connection

Sign in to your ONES tenant from [ONES](https://ones.cn/). In **Personal Settings > Personal API keys**, create a key with `read:account:teams` scope and copy its one-time-shown secret into Dex `api_key`. The [official key guide](https://docs.ones.cn/developer/openapi/auth/personal-api-keys/) describes the path and Bearer authentication. Set `baseUrl` to the HTTPS origin of your ONES tenant, without a path. Leave timeout and response limit blank for displayed defaults. Revoke a compromised key in Personal API keys and create a replacement. The key represents its creating user and is suited to user-level integrations.

## Go

The [team catalog Flow](go/examples/team-catalog/) is a runnable Worker with static Dex Web connection name `ones`. `listed` completes the Flow; optional branches fail unless an application wires a business response. Transient failures retry safely. The local mock integration uses synthetic team data and a real Dex Worker and Client. Live authorization and team visibility remain unverified without a user-controlled tenant connection.

## Python

The first Python package implements the read-only operation and all Go-recorded provider branches. See [Python package](python/README.md) and its [runnable Flow](python/examples/team_list/README.md). Live provider authorization and responses are unverified.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/ones/TeamDirectoryWorker.java) registers `JavaONESTeamDirectory`. Run `./gradlew teamDirectoryWorker` from `java/` with `ONES_API_KEY` supplied through your credential manager. Set `ONES_BASE_URL` to the configured provider origin. Hosted Java applications can use `TeamDirectory.fromProject` with their loaded connection.

The [Rust Worker](rust/examples/list_teams.rs) registers `RustONESTeamDirectory`. From `rust/`, run `cargo run --example list_teams` with `DEX_CONNECTOR_CONFIG_FILE` pointing at the connections file exported by Dex Web. Both examples use the static `ones` connection name and `DEX_FLOW_SERVICE_ADDRESS` to select the Dex server. Start the Flow with an empty JSON object.

Generate and lint the Flow Definition Graphs from the connector directory.

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/ones/TeamDirectoryWorker.java --language java --schema-version 2.0 --lint app --open=false --json --out /tmp/ones-java
dexcli visualize rust/examples/list_teams.rs --language rust --schema-version 2.0 --lint app --open=false --json --out /tmp/ones-rust
```
