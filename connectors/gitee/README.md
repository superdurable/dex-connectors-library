# Gitee connector

The Go connector reads one repository through Gitee's [REST API](https://gitee.com/api/v5/swagger). It exposes the `GetRepository` Query Step and a runnable [`repository-lookup` Flow](go/examples/repository-lookup/README.md). Provide the owner and repository paths from `https://gitee.com/<owner>/<repo>` in the Flow input.

Create a personal access token in [Gitee Personal Access Tokens](https://gitee.com/profile/personal_access_tokens) with the minimum repository-read access needed. Dex stores it as a secret connection credential. Gitee's REST API accepts it as `access_token` in the HTTPS query. The connector disables redirects and does not put URLs, credentials, or provider error bodies into failures or results.

The Query retries HTTP 408, 429, 5xx, and transport failures under Dex's default retry budget. HTTP 404 selects `notFound`; other non-200 statuses select `providerRejected`. Malformed or oversized responses select `invalidResponse`; invalid input or credentials select `defect`.

Install `github.com/superdurable/dex-connectors-library/connectors/gitee/go@v0.1.0` after release. Provider tests use an HTTP test server. The mock-backed example integration test uses a real Dex Worker and Client. Live personal-token authentication and private-repository lookup require a user-configured token and remain unverified by local tests.

The [Python package](python/README.md) implements `getRepository` and includes a [runnable Dex Flow](python/examples/repository_lookup/README.md). Its provider scenarios replay the Go-recorded contract; live provider authorization remains unverified without user-controlled credentials.

The [TypeScript package](typescript/README.md) implements the same `getRepository` Query and includes a [runnable Dex Flow](typescript/examples/basic/README.md). Its provider scenarios replay the Go-recorded contract; live provider authorization remains unverified without user-controlled credentials.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/gitee/RepositoryWorker.java) and [Rust Worker](rust/examples/get_repository.rs) run `getRepository` through the `gitee` connection. The Flow accepts the repository URL's owner and repository path segments as `owner` and `repo`. It returns the selected metadata and omits nested owner details.

Run `./gradlew repositoryWorker` from `java/` with `GITEE_ACCESS_TOKEN` supplied through the Worker environment. The example holds its connection in an in-memory Dex project for the life of the process. Applications using hosted project configuration can bind their loaded project with `GetRepositoryOperation.fromProject`.

Run `cargo run --example get_repository` from `rust/` to load the connection from Dex project configuration. For local Dex Web, `DEX_CONNECTOR_CONFIG_FILE` selects the connections file. Both Workers use `DEX_FLOW_SERVICE_ADDRESS`, defaulting to `127.0.0.1:8801`.

With the Worker running, start `JavaGiteeGetRepository` or `RustGiteeGetRepository` and pass a repository visible to the token. To generate FDG 2.0 from this connector directory:

```sh
dexcli visualize java/examples/io/superdurable/dexconnectors/gitee/GetRepositoryFlow.java --schema-version 2.0 --lint app --json --open=false --out /tmp/gitee-java
dexcli visualize rust/examples/get_repository.rs --schema-version 2.0 --lint app --json --open=false --out /tmp/gitee-rust
```
