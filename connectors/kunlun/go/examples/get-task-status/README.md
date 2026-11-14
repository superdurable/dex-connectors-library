# Read Mureka song task status

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `kunlun` connection in Dex Web using a Mureka API key. Start `MurekaTaskStatus` with `{"taskId":"<existing-song-task-id>"}`; get the ID from a Mureka song-generation response. The default mock uses `435134` and returns a succeeded task.

Generate and lint a strict FDG 2.0 graph from the runnable source:

```sh
dexcli visualize -json -schema-version 2.0 -lint app -out flow/flow.fdg flow/workflow.go
```

Use `GOWORK=off go test ./...` for deterministic checks. Run `GOWORK=off go test -tags=integration ./examples/get-task-status/flow -run TestGetTaskStatusUsesDefaultMockWithRealDex -count=1` with a local `dexcli dev` server for real Dex mock integration.
