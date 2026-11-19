# GitLab lookup example

This runnable Worker registers `GitLabLookup` and `GitLabIssueLookup` using the static connection name `gitlab`. The first calls `gitlab.NewGetProjectStep`; the second calls `gitlab.NewGetIssueStep`. Optional branches are unwired and fail the Flow if selected. Its [mock-backed integration test](flow/workflow_mock_integration_test.go) exercises both through a real Dex Worker and Client.

## Generate the strict Flow Definition

From `connectors/gitlab`, use the latest stable dexcli:

```bash
mkdir -p build
dexcli visualize ./examples/gitlab-lookup/flow/workflow.go --schema-version 2.0 --json --out ./build/gitlab-lookup
dexcli visualize ./examples/gitlab-lookup/flow/issue_lookup.go --schema-version 2.0 --lint app --json --out ./build/gitlab-issue-lookup
```

The definition must report `valid: true`. The local module can warn that a connector release is required.

## Configure and run

From the repository root, create release metadata for the local source:

```bash
mkdir -p /tmp/gitlab-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/gitlab/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/gitlab/go \
  --version v0.3.0 --tag connectors/gitlab/go/v0.3.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/gitlab-release/connector-release.json \
  --digest-output /tmp/gitlab-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/gitlab/go/build" --connector-release-override gitlab=/tmp/gitlab-release
```

In Dex Web **Connections**, select `gitlab / gitlab`, follow the authorization guide beginning at [https://gitlab.com/-/user_settings/personal_access_tokens](https://gitlab.com/-/user_settings/personal_access_tokens), and save the connection. Keep keys in secret fields only. From `connectors/gitlab`, run `go run ./examples/gitlab-lookup` with the `DEX_PROJECT_*` environment described in [projectconfig](../../../../../sdkgo/projectconfig/README.md#application-loading). In Dex Web **Start Flow**, select `GitLabLookup` and enter this JSON request:

```json
{
  "projectId": "meridian/web"
}
```

Use the input type `GetProjectInput` as the JSON shape; the provider-specific fields are described in the [connector README](../../../README.md). The Worker accepts `DEX_FLOW_SERVICE_ADDRESS` and `DEX_BLOB_CACHE_DIR` overrides.

For `GitLabIssueLookup`, enter `{"projectId":"meridian/web","issueIid":7}`. The `issueIid` is the positive project-local number shown in GitLab's issue URL.

Live provider authorization has not been exercised locally. The mock-backed integration test has passed against local Dex.
