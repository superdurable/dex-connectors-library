# JiHu GitLab project catalog Flow

This runnable Flow uses static connection `jihugitlab` and reads one project page. The `listed` branch stores the output in a Dex Attribute and completes. Optional failure branches are unwired and fail the Flow.

From `connectors/jihugitlab/go`, generate and lint the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/project-catalog/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/jihugitlab-project-catalog
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override jihugitlab=<artifact-directory>`. In Dex Web Connections, create `jihugitlab` using the [connection guide](../../../README.md#connection). With `DEX_PROJECT_*` configured, run `GOWORK=off go run ./examples/project-catalog` and start `JiHu GitLabProjectCatalog` with `{}`.

For the local synthetic mock, run `dexcli dev -dex-port 10301 -web-port 10302 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10301 GOWORK=off go test -tags integration ./examples/project-catalog/flow -run TestProjectCatalogUsesDefaultMockWithRealDex -count=1`.
