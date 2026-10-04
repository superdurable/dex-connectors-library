# GitHub repository changes example

This operation-only example reads recent changes in one public GitHub
repository and returns a bounded report:

1. `ListMergedPullRequests` reads the 10 newest pull requests merged in the
   requested window;
2. `ListPullRequestFiles` reads up to 20 changed files for each of the 3 newest
   of those pull requests, one Step execution per pull request; and
3. `ListCommits` reads the 20 newest default-branch commits in the same window.

Application Steps store the report in the `github-repository-changes-report`
Attribute before each GitHub call, because a Connector branch target receives
only the current operation Result. The summary and display RPCs read that
Attribute for Dex Web. Each Connector Step makes one GitHub request, so a
retry repeats only that read.

The example wires only the `listed` branches. An unwired optional branch, such
as `providerRejected` for a repository GitHub cannot search, fails the Flow.
A rate-limited read is retried after GitHub's `Retry-After` or
`X-RateLimit-Reset` delay, or after one minute when GitHub sends neither. Each
GitHub Step can wait up to 65 minutes in total, which covers GitHub's hourly
primary rate-limit window; a later reset fails the Flow. The report records
whether more pull requests, files, or commits exist, but the example does not
follow the next pages.

The Flow sets its type to `GitHubRepositoryChanges` and names each application
Step explicitly. Dex Web **Start Flow** starts the Flow and Step types written
in the Flow Definition Graph, which omit the Go package, while the Go SDK
otherwise registers package-qualified defaults. The start Step also keeps a
`WaitFor` that returns at once, because Dex Web Start Flow in dexcli v0.13.8
invokes `WaitFor` on the start Step and an execute-only Step rejects it.

## Release baseline

- GitHub Connector `v0.21.0`
- dexcli `v0.13.8` or a later stable release

## 1. Prepare a clean local test project

Dex Web configures a connection only for an exact released Connector module.
Create a separate project that consumes the release, then copy the Flow source
so dexcli analyzes it as an application dependency:

```bash
mkdir github-repository-changes-e2e
cd github-repository-changes-e2e
mkdir -p flow build

curl -fsSL \
  https://raw.githubusercontent.com/superdurable/dex-connectors-library/refs/tags/connectors/github/v0.21.0/connectors/github/examples/repository-changes/flow/workflow.go \
  -o flow/workflow.go

go mod init example.com/github-repository-changes-e2e
go mod edit -go=1.24.0
go get github.com/superdurable/dex-connectors-library/connectors/github@v0.21.0
go mod tidy
```

`go mod edit -go=1.24.0` keeps the project's Go version at or below the Go
release that built dexcli. Otherwise dexcli reports `go_type_check_failed`.

Generate the Flow Definition Graph used by Dex Web:

```bash
dexcli visualize ./flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/github-repository-changes
```

The command must finish without blocking diagnostics and create
`build/github-repository-changes.json` with `"valid": true`. The repository's
compatibility gate runs the same analysis twice from a clean consumer module
and requires identical output.

## 2. Start Dex

```bash
dexcli dev \
  --flow-rendering-dir "$PWD/build"
```

Record the Dex Web URL and Dex Server address that dexcli prints. With the
default ports they are `http://127.0.0.1:8802` and `127.0.0.1:8801`.

## 3. Create a GitHub OAuth App

In GitHub, open **Settings** > **Developer settings** > **OAuth Apps** and
choose **New OAuth App**:

- **Homepage URL**: the Dex Web URL, such as `http://127.0.0.1:8802`;
- **Authorization callback URL**: the Dex Web URL followed by
  `/api/v2/connector-oauth/callback`, such as
  `http://127.0.0.1:8802/api/v2/connector-oauth/callback`.

Register the app, then generate a client secret. The Connector requests only
`read:user user:email`, which reads public repositories. It rejects a token
whose scopes differ, including one that also grants `repo`.

## 4. Configure the connection in Dex Web

Open **Connections** and select `github / github-repository-changes`. It lists
the `listMergedPullRequests`, `listPullRequestFiles`, and `listCommits` uses.
Enter the OAuth client ID and secret, leave the configuration defaults, and
choose **Authorize**. After GitHub returns to Dex Web, the connection status
should be **Ready**.

## 5. Run the Worker

The Worker reads the `DEX_PROJECT_*` project configuration environment
described in [project configuration](../../../../sdkgo/projectconfig/README.md);
Dex Web or Superverse Studio writes that configuration when you save the
connection.

```bash
export DEX_FLOW_SERVICE_ADDRESS="127.0.0.1:8801"

GOWORK=off go run \
  github.com/superdurable/dex-connectors-library/connectors/github/examples/repository-changes@v0.21.0
```

The Worker listens on `127.0.0.1:8816` by default. Set
`DEX_WORKER_BIND_ADDRESS` to use another address, and `DEX_BLOB_CACHE_DIR` to
move its blob cache. If Dex is unreachable, the Worker logs
`dex server unavailable; retrying` with a delay that grows to 30 seconds and
starts once Dex answers.

## 6. Start the Flow

In Dex Web, open the `GitHubRepositoryChanges` Flow, choose **Start Flow**,
select the Worker address, and enter:

```json
{
  "owner": "superdurable",
  "repository": "dex",
  "windowStart": "2026-09-19T00:00:00Z",
  "windowEnd": "2026-09-26T00:00:00Z"
}
```

Both window times are RFC 3339 timestamps, and the start must be before the
end. Open the run and follow it to **Completed**. The display shows the report
with the merged pull requests, their changed files, and the commits.

## Verify from the repository

The example tests run from the Connector directory:

```bash
GOWORK=off go test -race ./examples/...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  GOWORK=off go test -tags=integration ./examples/repository-changes/flow/...
```

The integration tests use a real Dex Server and a local fake GitHub API. They
verify a completed report, a rate-limited search that Dex retries after
GitHub's `Retry-After` delay, a duplicate start with the same request ID, an
empty repository, a Flow failure for an unwired `providerRejected` branch, and
a Flow failure without waiting when GitHub's rate-limit reset is past the
Step's retry budget.

Before `v0.21.0` is published, build a local release artifact with
`go run ./cmd/connectorctl release-artifact` from the repository root and pass
its directory to `dexcli dev --connector-release-override github=DIRECTORY`.
The test project must still resolve `connectors/github@v0.21.0` without a
`replace`, as the compatibility gate does with a local module proxy.
