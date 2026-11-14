# OceanBase Cloud instance list Flow

This runnable Flow uses the static `oceanbase` connection to list one page of project instances. The `listed` branch stores the result in a Dex Attribute and completes with it. Optional branches are unwired and fail the Flow when selected.

Start a local Dex development server:

```sh
dexcli dev -dex-port 9471 -web-port 9472 -open=false
```

In Dex Web, add an OceanBase Cloud connection using the AccessKey ID, Secret, and Project ID from the OceanBase Cloud console. Choose the service site where the instance was ordered. With the project's `DEX_PROJECT_*` environment set, run:

```sh
GOWORK=off go run .
```

In Dex Web **Start Flow**, choose `OceanBaseInstanceList`, enter a unique Flow ID, and use input such as `{"pageNumber":1,"pageSize":10}`. The output contains instance `items` and `total`. The mock-backed integration test uses a real Dex Worker and Client without an OceanBase Cloud AccessKey.

Generate and lint a strict FDG 2.0 graph:

```sh
dexcli visualize -json -schema-version 2.0 -lint app -out flow/flow.fdg flow/workflow.go
```

Run `GOWORK=off go test ./...` for deterministic checks. Run `GOWORK=off go test -tags=integration ./examples/instance-list/flow -run TestInstanceListUsesDefaultMockWithRealDex -count=1` against `dexcli dev` for real Dex mock integration.
