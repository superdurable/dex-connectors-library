# Workday worker inspection

These Go Flows call `workday.NewGetWorkerStep` for one worker WID and `workday.NewListWorkersStep` for one worker page. Each stores its typed result in a Dex Attribute before completion. The static Dex Web connection name is `workday`.

From the repository root, generate the strict FDG 2.0 graph:

```sh
dexcli visualize connectors/workday/go/examples/worker-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/workday-fdg --lint app
dexcli visualize connectors/workday/go/examples/worker-inspection/flow/worker_search.go --schema-version 2.0 --json --out /tmp/workday-search-fdg --lint app
```

Create a Workday connection in Dex Web as described in [the connector README](../../../README.md), then run `go run ./examples/worker-inspection` from `connectors/workday/go`. Start `WorkdayWorkerInspection` with JSON input such as `{"workerId":"0123456789abcdef0123456789abcdef"}`. The ID must be a WID of a worker accessible to the authorized user. Start `WorkdayWorkerSearch` with `{"limit":5,"offset":0,"search":"Ada"}` to read the first matching page. The mock integration test exercises both Flows against a local Dex stack without live Workday credentials.
