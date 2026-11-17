# Bitbucket resource-read example

This example registers two Flows on the static `bitbucket` connection: `BitbucketResourceRead` lists repositories, and `BitbucketPullRequestTitleUpdate` replaces one pull request title through `bitbucket.NewUpdatePullRequestTitleStep`. Start input is typed as `Request`; the result is `Outcome`. Optional error branches are unwired and fail the Flow.

## Generate strict FDG 2.0

From `connectors/atlassian/bitbucket`:

```sh
mkdir -p build
dexcli visualize ./examples/resource-read/flow/workflow.go --schema-version 2.0 --json --out ./build/resource-read
dexcli visualize ./examples/resource-read/flow/title_workflow.go --schema-version 2.0 --json --out ./build/title-update
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

The deterministic unit test checks input mapping and Flow identity. `workflow_mock_integration_test.go` runs both manifest defaults through a real Dex Worker in CI and can run locally with `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:9140 GOWORK=off go test -tags=integration ./examples/resource-read/flow -run '^TestPullRequestTitleUsesManifestMockWithRealDex
 -count=1 -v`.
