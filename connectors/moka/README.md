# Moka connector

This connector implements Moka's read-only [job function list](https://www.mokahr.com/docs/api/index.html#-26). It requests `GET https://api.mokahr.com/api-platform/v1/jobs-groupedby-zhineng/{orgId}` with the selected `social` or `campus` recruitment mode. Its output contains only function ID, label, parent ID, and job count. It excludes candidate details, recruiter details, and provider error bodies.

[Moka's API guide](https://www.mokahr.com/docs/api/index.html#basic-auth) says the private API Key is issued by the organization's Customer Success Manager, is used as the HTTP Basic Auth username with an empty password, and all requests must use HTTPS. Ask the CSM for the key and enter it as a Dex secret. In Moka Recruitment Website Management, copy the recruitment website link; the `orgId` is the segment after `/apply/` in `https://app.mokahr.com/apply/{orgId}/{siteId}`. Leave mode blank to use social hiring, or select campus hiring. Set the Dex worker's outbound IP in Moka's Open API allowlist if your organization uses it.

The runnable [job-function example](go/examples/list-job-functions/README.md) demonstrates the generated operation-specific Step and static `moka` connection. Real Moka authorization remains to be tested with a user-controlled Dex Web Connection.

The [Python package](python/README.md) implements the same Query with a checked-in Dex Worker and Client example. Its Go-recorded provider scenarios and local Dex retry path are verified; live provider authorization remains unverified.
The [TypeScript package](typescript/README.md) exposes `listJobFunctions` and includes a runnable Dex Flow, Go scenario replay, and a local provider integration test.

A job function with an omitted or null parent ID has `parentId` equal to zero.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/moka/JobFunctionDirectoryWorker.java) registers `JavaMokaJobFunctionDirectory`. Run `./gradlew jobFunctionDirectoryWorker` from `java/` with `MOKA_API_KEY` and the organization identifier in `MOKA_ORG_ID` (set `MOKA_MODE` for campus recruiting) supplied through your credential manager. Hosted Java applications can use `JobFunctionDirectory.fromProject` with their loaded connection.

The [Rust Worker](rust/examples/list_job_functions.rs) registers `RustMokaJobFunctionDirectory`. From `rust/`, run `cargo run --example list_job_functions` with `DEX_CONNECTOR_CONFIG_FILE` pointing at the connections file exported by Dex Web. Both examples use the static `moka` connection name and `DEX_FLOW_SERVICE_ADDRESS` to select the Dex server. Start the Flow with an empty JSON object.

Generate and lint the Flow Definition Graphs from the connector directory.

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/moka/JobFunctionDirectoryWorker.java --language java --schema-version 2.0 --lint app --open=false --json --out /tmp/moka-java
dexcli visualize rust/examples/list_job_functions.rs --language rust --schema-version 2.0 --lint app --open=false --json --out /tmp/moka-rust
```
