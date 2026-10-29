# GitLab lookup example

This runnable Flow calls `gitlab.NewGetProjectStep` with the static connection name `gitlab` and completes with the typed `Project` value. Optional branches are unwired and fail the Flow if selected. Its [mock-backed integration test](flow/workflow_mock_integration_test.go) uses a real Dex Worker and Client in CI.

## Generate the strict Flow Definition

From `connectors/gitlab`, use the latest stable dexcli:

```bash
mkdir -p build
dexcli visualize ./examples/gitlab-lookup/flow/workflow.go --schema-version 2.0 --json --out ./build/gitlab-lookup
```

The definition must report `valid: true`. The local module can warn that a connector release is required.

## Configure and run

From the repository root, create release metadata for the local source:

```bash
mkdir -p /tmp/gitlab-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/gitlab/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/gitlab \
  --version v0.1.0 --tag connectors/gitlab/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/gitlab-release/connector-release.json \
  --digest-output /tmp/gitlab-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/gitlab/build" --connector-release-override gitlab=/tmp/gitlab-release
```

In Dex Web **Connections**, select `gitlab / gitlab`, follow the authorization guide beginning at [https://gitlab.com/-/user_settings/personal_access_tokens](https://gitlab.com/-/user_settings/personal_access_tokens), and save the connection. Keep keys in secret fields only. From `connectors/gitlab`, run `go run ./examples/gitlab-lookup` with the `DEX_PROJECT_*` environment described in [projectconfig](../../../../sdkgo/projectconfig/README.md#application-loading). In Dex Web **Start Flow**, select `GitLabLookup` and enter this JSON request:

```json
{
  "projectId": "meridian/web"
}
```

Use the input type `GetProjectInput` as the JSON shape; the provider-specific fields are described in the [connector README](../../README.md). The Worker defaults to `127.0.0.1:8873` and accepts `DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, and `DEX_BLOB_CACHE_DIR` overrides.

Live provider authorization and the real Dex example have not been exercised locally; CI runs the mock-backed integration test.
