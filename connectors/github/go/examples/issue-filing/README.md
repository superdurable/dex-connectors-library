# GitHub issue filing example

This operation-only example files one issue, such as a bug report, unless an
open issue with the same title is already tracked:

1. `ListIssues` reads open issues that carry every requested label, 100 per
   page, and follows `NextPage` for at most 3 pages;
2. `recordOpenIssues` completes the Flow with the existing issue when a title
   matches, ignoring case and surrounding whitespace; and
3. `CreateIssue` creates the issue once, then the Flow completes with its
   number, node ID, and URL, unless the input asks for follow-ups;
4. when the input sets `comment`, `CommentOnIssue` adds it to the created issue
   once, reading back a retried send instead of posting a duplicate;
5. when the input sets `relabelWith`, `RelabelIssue` replaces the created
   issue's labels with those names; and
6. when the input sets `closeWithStateReason` to `completed` or `not_planned`,
   `CloseIssue` closes the created issue with that reason, filing a problem
   that is already resolved, and the Flow completes as `closed`. Any other
   nonempty reason fails the Flow before GitHub is called.

The `github-issue-filing-report` Attribute records the request, the pages read,
and the existing or created issue, because a Connector branch target receives
only the current operation Result. The summary and display RPCs read it for
Dex Web.

The example wires only the `listed`, `created`, `replaced`, and `updated` branches. An unwired optional
branch fails the Flow, such as `insufficientScope` on a connection that does not
use the `github-oauth-repository` method, or `uncertain` when every allowed
send ended without a confirmed issue. `CreateIssue` never files a duplicate when
its Step retries: a retry first reads the saved issue number, or lists the
newest issues the connection's account created and matches the hidden marker in
their bodies, and sends again only while the send cap allows. `CommentOnIssue` follows the same read-back
rule for comments. `RelabelIssue` and `CloseIssue` set an absolute label set
and state, so a retried Step sends the same update again. The title check
covers the first 300 open issues with the labels; `hasUncheckedOpenIssues`
reports more.

The Flow sets its type to `GitHubIssueFiling` and names each application Step
explicitly, so Dex Web **Start Flow** starts the types written in the Flow
Definition Graph. The start Step keeps a `WaitFor` that returns at once,
because Dex Web Start Flow invokes `WaitFor` on the start Step.

## Release baseline

- GitHub Connector `v0.25.0`
- dexcli `v1.47.3` or a later stable release

## 1. Prepare a clean local test project

Dex Web configures a connection only for an exact released Connector module.
Create a separate project that consumes the release, then copy the Flow source
so dexcli analyzes it as an application dependency:

```bash
mkdir github-issue-filing-e2e
cd github-issue-filing-e2e
mkdir -p flow build

curl -fsSL \
  https://raw.githubusercontent.com/superdurable/dex-connectors-library/refs/tags/connectors/github/go/v0.25.0/connectors/github/go/examples/issue-filing/flow/workflow.go \
  -o flow/workflow.go

go mod init example.com/github-issue-filing-e2e
go mod edit -go=1.24.0
go get github.com/superdurable/dex-connectors-library/connectors/github/go@v0.25.0
go mod tidy
```

Generate the Flow Definition Graph used by Dex Web:

```bash
dexcli visualize ./flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/github-issue-filing
```

The command must finish without blocking diagnostics and create
`build/github-issue-filing.json` with `"valid": true`.

## 2. Start Dex

```bash
dexcli dev \
  --flow-rendering-dir "$PWD/build"
```

Record the Dex Web URL and Dex Server address that dexcli prints. With the
default ports they are `http://127.0.0.1:8802` and `127.0.0.1:8801`.

## 3. Create a GitHub OAuth App

In GitHub, open <https://github.com/settings/developers>, then **OAuth Apps**,
and choose **New OAuth App**:

- **Homepage URL**: the Dex Web URL, such as `http://127.0.0.1:8802`;
- **Authorization callback URL**: the Dex Web URL followed by
  `/api/v2/connector-oauth/callback`, such as
  `http://127.0.0.1:8802/api/v2/connector-oauth/callback`.

Register the app, then generate a client secret. The `github-oauth-repository`
method requests `repo offline_access`. `repo` is the only GitHub OAuth App scope
that writes issues in private repositories, and it also reads and writes code
and pull requests in every repository the authorizing account can reach, so
authorize with an account limited to the repositories you file issues in. When
the repository belongs to an organization that restricts OAuth App access, an
organization owner approves the app under the organization's **Settings** >
**Third-party Access** > **OAuth app policy**.

## 4. Configure the connection in Dex Web

Open **Connections** and select `github / github-issue-filing`. It lists the
`listIssues`, `createIssue`, `createIssueComment`, `replaceIssueLabels`, and
`setIssueState` uses. Choose the **GitHub OAuth, repository
issues** method, enter the OAuth client ID and secret, leave the configuration
defaults, and choose **Authorize**. Approve the `repo` scope on GitHub. After
GitHub returns to Dex Web, the connection status should be **Ready**. The
default **GitHub OAuth, public read-only** method cannot create issues: the run
fails at `CreateIssue` with `insufficientScope` and sends nothing.

## 5. Run the Worker

The Worker reads the `DEX_PROJECT_*` project configuration environment
described in [project configuration](../../../../../sdkgo/projectconfig/README.md);
Dex Web writes that configuration when you save the connection.

```bash
export DEX_FLOW_SERVICE_ADDRESS="127.0.0.1:8801"

GOWORK=off go run \
  github.com/superdurable/dex-connectors-library/connectors/github/go/examples/issue-filing@v0.25.0
```

Set `DEX_BLOB_CACHE_DIR` to move its blob cache. If Dex is unreachable, the
Worker logs `dex server unavailable; retrying` with a delay that grows to 30
seconds and starts once Dex answers.

## 6. Start the Flow

In Dex Web, open the `GitHubIssueFiling` Flow, choose **Start Flow**, and enter
a repository the authorizing account can write issues in:

```json
{
  "owner": "octocat",
  "repository": "hello-world",
  "title": "Crash when saving an empty draft",
  "body": "Saving a new draft without text closes the editor.",
  "labels": ["bug"]
}
```

Open the run and follow it to **Completed**. The display shows `created` with
the new issue's number and URL. Start the Flow again with a new Flow ID and the
same input: it completes as `alreadyTracked` with the same issue and creates
nothing. Start a third run with a new title, `"comment": "Fixed in the
latest build."`, `"relabelWith": ["bug", "fixed"]`, and `"closeWithStateReason":
"completed"`: it completes as `closed`, and the new issue carries the comment
and the two labels and is closed as completed. Close the first test issue on GitHub afterwards.

## Verify from the repository

The example tests run from the Connector's `go` directory:

```bash
GOWORK=off go test -race ./examples/issue-filing/...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  GOWORK=off go test -tags=integration ./examples/issue-filing/flow/...
```

The integration tests use a real Dex Server and a local fake GitHub API. They
verify a created issue after two pages of open issues, an already tracked title
on the second page, a create that GitHub accepted but answered with `502`
whose Step retry reads the issue back instead of sending again, a create that
GitHub never stored whose retry reads back and then sends once more, a Flow
failure without any create on a `github-oauth` connection, a created issue that
the Flow closes as not planned after a `502` close whose Step retry sends the
same state again, a reopen reason that fails the Flow before any request, and
a comment, relabel, and close of the filed issue where GitHub stores the comment
but answers `502` and the first read-back fails, so the Step retry reads the
comment back from the newest page and posts nothing again. A mock test runs
the same Flow on a `githubmock` connection through all three default issue
pages, an already tracked title, a created issue that the default
`setIssueState` mock closes, a created issue that the default comment and label
mocks follow up, and a scripted `uncertain` create that fails the
Flow.

Before `v0.25.0` is published, build a local release artifact from the
repository root and pass its directory to dexcli:

```bash
mkdir -p /tmp/github-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/github/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/github/go \
  --version v0.25.0 --tag connectors/github/go/v0.25.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/github-release/connector-release.json \
  --digest-output /tmp/github-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/build" --connector-release-override github=/tmp/github-release
```

The test project must still resolve `connectors/github/go@v0.25.0` without a
`replace`, as the compatibility gate does with a local module proxy.
