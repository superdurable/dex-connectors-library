# List Worktile projects

Run `go run .` from this directory with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `worktile` connection in Dex Web using a Worktile application Client ID and Client Secret. Start `WorktileProjectList` with `{"pageIndex":0,"pageSize":10}`; it returns one page of projects. The example's default mock returns two projects without calling Worktile.

Generate and lint a strict FDG 2.0 graph from the runnable source:

```sh
dexcli visualize -json -schema-version 2.0 -lint app -out - flow/workflow.go
```

Use `GOWORK=off go test ./...` for deterministic checks. Run `GOWORK=off go test -tags=integration ./examples/list-projects/flow -run TestListProjectsUsesDefaultMockWithRealDex -count=1` with a local `dexcli dev` server for real Dex mock integration.
