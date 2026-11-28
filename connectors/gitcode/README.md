# GitCode repository lookup

The Go connector performs the official read-only [repository lookup](https://docs.gitcode.com/docs/apis/get-api-v-5-repos-owner-repo/). It returns only repository ID, full name, visibility, and default branch; creator email, membership, and other private response fields are dropped.

GitCode [API authentication](https://docs.gitcode.com/docs/apis/) accepts a personal access token in an HTTPS `Authorization: Bearer` header. Create a token in [personal settings](https://docs.gitcode.com/docs/help/home/user_center/security_management/user_pat/). The default rate limit is 400 requests per minute and 4000 per hour.

`getRepository` selects `fetched`, `notFound`, `providerRejected`, `invalidResponse`, or `defect`. Rate limits, transport errors, and temporary server faults retry. Optional branches fail the example Flow unless wired by an application.

[Run the repository Flow](go/examples/repository/README.md). Its checked-in mock is synthetic; live repository lookup has not been verified without a user-controlled token in Dex Web Connections.

The [Python package](python/README.md) includes a [runnable Dex Flow](python/examples/repository_lookup/README.md) and replays the Go-recorded provider scenarios. Live provider authorization remains unverified without user-controlled credentials.

The [TypeScript package](typescript/README.md) includes a [runnable Dex Flow](typescript/examples/basic/README.md) and replays the Go-recorded provider scenarios. Live provider authorization remains unverified without user-controlled credentials.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/gitcode/RepositoryWorker.java) and [Rust Worker](rust/examples/get_repository.rs) run `getRepository` through the `gitcode` connection. The Flow accepts the repository URL's owner and repository path segments as `owner` and `repo`. It returns the selected metadata and omits nested owner details.

Run `./gradlew repositoryWorker` from `java/` with `GITCODE_ACCESS_TOKEN` supplied through the Worker environment. The example holds its connection in an in-memory Dex project for the life of the process. Applications using hosted project configuration can bind their loaded project with `GetRepositoryOperation.fromProject`.

Run `cargo run --example get_repository` from `rust/` to load the connection from Dex project configuration. For local Dex Web, `DEX_CONNECTOR_CONFIG_FILE` selects the connections file. Both Workers use `DEX_FLOW_SERVICE_ADDRESS`, defaulting to `127.0.0.1:8801`.

With the Worker running, start `JavaGitCodeGetRepository` or `RustGitCodeGetRepository` and pass a repository visible to the token. To generate FDG 2.0 from this connector directory:

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/gitcode/GetRepositoryFlow.java --schema-version 2.0 --lint app --json --open=false --out /tmp/gitcode-java
dexcli visualize rust/examples/get_repository.rs --schema-version 2.0 --lint app --json --open=false --out /tmp/gitcode-rust
```
