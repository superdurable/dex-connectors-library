# Workday worker inspection

This Go Flow calls `workday.NewGetWorkerStep` for one worker WID and stores the selected `WorkerSummary` in a Dex Attribute before completion. The static Dex Web connection name is `workday`.

From the repository root, generate the strict FDG 2.0 graph:

```sh
dexcli visualize connectors/workday/go/examples/worker-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/workday-fdg --lint app
```

Create a Workday connection in Dex Web as described in [the connector README](../../../README.md), then run `go run ./examples/worker-inspection` from `connectors/workday/go`. Start `WorkdayWorkerInspection` with JSON input such as `{"workerId":"0123456789abcdef0123456789abcdef"}`. The ID must be a WID of a worker accessible to the authorized user. The example's mock integration test exercises the same Flow against a local Dex stack without live Workday credentials.
