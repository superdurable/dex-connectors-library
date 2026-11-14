# Read Vidu task status

Run `go run .` from this directory with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `shengshu` connection in Dex Web using a Vidu API key. Start `ViduTaskStatus` with `{"taskId":"<existing-task-id>"}`; the ID comes from a Vidu generation response or task list. The default mock uses `task-123` and returns a completed task with one creation ID.

Generate and lint a strict FDG 2.0 graph from the runnable source:

```sh
dexcli visualize -json -schema-version 2.0 -lint app -out flow/flow.fdg flow/workflow.go
```

Use `GOWORK=off go test ./...` for deterministic checks. Run `GOWORK=off go test -tags=integration ./examples/get-task-status/flow -run TestGetTaskStatusUsesDefaultMockWithRealDex -count=1` with a local `dexcli dev` server for real Dex mock integration.
