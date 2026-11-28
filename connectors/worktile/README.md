# Worktile connector

This connector implements the current Worktile public-cloud [paged project query](https://worktile.apifox.cn/api-101771901). It obtains a short-lived tenant token from the fixed HTTPS [token endpoint](https://worktile.apifox.cn/api-101771895) for each call, then requests one page of projects. `PageIndex` starts at zero; `PageSize` is 1–100. The output includes project ID, name, code, visibility, and `HasMore`; it does not include member IDs or descriptions.

A Worktile administrator creates an app under **Management Backend > Application Management > My Apps** and copies its Client ID and Client Secret into Dex Web Connections. Tenant authorization grants team-wide access. The connector sends both credentials only to the HTTPS token endpoint and the resulting token only to the fixed HTTPS project endpoint. It does not place credentials, tokens, raw URLs, provider errors, or member IDs in Flow state. Worktile documents a [360-call-per-minute limit](https://worktile.apifox.cn/) and a 7200-second tenant token lifetime. The connector fetches a fresh token for each Query instead of persisting it.

The runnable [project-list example](go/examples/list-projects/README.md) demonstrates the generated operation-specific Step and static `worktile` connection. Real Worktile authorization remains to be tested with a user-controlled Dex Web Connection.

## Python

The [`python/`](python/) package implements the `listProjects` Query. Its checked-in example runs a Worker and a real Dex retry test; provider authorization remains unverified.

## TypeScript

The TypeScript package under [`typescript/`](typescript/) implements `listProjects` with a runnable Dex Flow, Go-recorded conformance replay, and a real Dex integration test.

## Java and Rust

Both packages implement `listProjects` and bind it to a Dex Query Step. The result lists projects visible to the connection in provider order. Worktile exchanges the client ID and secret for a tenant token on each attempt. Supply a zero-based `pageIndex` and a `pageSize` from 1 to 100.

The [Java Worker](java/examples/io/superdurable/dexconnectors/worktile/ProjectDirectoryWorker.java) serves `JavaWorktileProjectDirectory`. From `java/`, run `./gradlew projectDirectoryWorker` with `WORKTILE_CLIENT_ID` and `WORKTILE_CLIENT_SECRET` supplied by your credential manager. Hosted Java applications can bind their loaded Dex project with `ProjectDirectory.fromProject(project, "worktile")`.

The [Rust Worker](rust/examples/list_projects.rs) serves `RustWorktileProjectDirectory`. Set `DEX_CONNECTOR_CONFIG_FILE` to the connections file exported by Dex Web with the named `worktile` connection. From `rust/`, run `cargo run --example list_projects`. Both examples use `DEX_FLOW_SERVICE_ADDRESS` to select the Dex server.

Generate the Flow Definition Graphs from the connector directory.

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/worktile/ProjectDirectoryWorker.java --language java --schema-version 2.0 --lint app --open=false --json --out /tmp/worktile-java
dexcli visualize rust/examples/list_projects.rs --language rust --schema-version 2.0 --lint app --open=false --json --out /tmp/worktile-rust
```
