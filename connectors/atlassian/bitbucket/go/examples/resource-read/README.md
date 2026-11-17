# Bitbucket resource-read example

This example registers three Flows on the static `bitbucket` connection: `BitbucketResourceRead` lists repositories, `BitbucketPullRequestTitleUpdate` replaces one pull request title, and `BitbucketRepositoryCreate` creates a repository through `bitbucket.NewCreateRepositoryStep`. Optional error branches are unwired and fail the Flow.

## Generate strict FDG 2.0

From `connectors/atlassian/bitbucket`:

```sh
mkdir -p build
dexcli visualize ./examples/resource-read/flow/workflow.go --schema-version 2.0 --json --out ./build/resource-read
dexcli visualize ./examples/resource-read/flow/title_workflow.go --schema-version 2.0 --json --out ./build/title-update
dexcli visualize ./examples/resource-read/flow/repository_workflow.go --schema-version 2.0 --json --out ./build/repository-create
```

The command should report `valid: true`. A local unpublished module can produce a release metadata warning.

## Configure and run

From the repository root, build a local release override, then start a Dex stack with the rendered graph:

```sh
mkdir -p /tmp/bitbucket-release
go run ./cmd/connectorctl release-artifact --manifest connectors/atlassian/bitbucket/connector.yaml --module-path github.com/superdurable/dex-connectors-library/connectors/atlassian/bitbucket/go --version v0.3.0 --tag connectors/atlassian/bitbucket/go/v0.3.0 --source-sha "$(git rev-parse HEAD)" --output /tmp/bitbucket-release/connector-release.json --digest-output /tmp/bitbucket-release/connector-release.json.sha256
dexcli dev -open=false -dex-port 9140 -web-port 9141 --flow-rendering-dir "$PWD/connectors/atlassian/bitbucket/go/build" --connector-release-override bitbucket=/tmp/bitbucket-release
```

In Dex Web **Connections**, create the `bitbucket` connection with the manifest's authorization guide. Then start the Worker in another terminal:

```sh
cd connectors/atlassian/bitbucket/go
go run ./examples/resource-read
```

The pull Worker connects outbound to Dex. In **Start Flow**, choose `BitbucketResourceRead`, a unique Flow ID, and input like:

```json
{"workspace":"meridian"}
```

The Flow completes with the provider value. To exercise the title update, start `BitbucketPullRequestTitleUpdate` with a unique Flow ID and `{"workspace":"meridian","repository":"api","pullRequestId":7,"title":"Add client and contract tests"}`. Grant the workspace token Pull requests > Write and use a disposable pull request for live testing. A missing resource selects the unwired `notFound` branch and fails the Flow.

To create a repository, start `BitbucketRepositoryCreate` with a unique Flow ID and `{"workspace":"meridian","repository":"api","name":"Meridian API","is_private":true}`. Replace the workspace with your Bitbucket workspace slug and choose a unique repository slug under that workspace. Grant the token Repositories Write/Admin and use a disposable workspace for a live run. A blank slug or name fails validation; `is_private` must be explicit. Bitbucket creates a Git repository and assigns the oldest workspace project when no project is supplied.

The deterministic unit test checks input mapping and Flow identity. `workflow_mock_integration_test.go` runs the manifest defaults through a real Dex Worker in CI. Locally, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:9210 GOWORK=off go test -tags=integration ./examples/resource-read/flow -run '^TestRepositoryCreateUsesManifestMockWithRealDex$' -count=1 -v` while the dedicated Dex stack is running.
