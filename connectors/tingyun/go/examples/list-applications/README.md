# List Tingyun mobile applications

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `tingyun` connection in Dex Web with your HTTPS Tingyun platform origin and secret accessToken from **账户管理 > API**. Start `TingyunApplications` with `{}`. The default mock returns one application ID and name.

Generate and lint strict FDG 2.0 from the runnable source:

```sh
dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/tingyun-flow.fdg flow/workflow.go
```

Use `GOWORK=off go test ./...` for deterministic checks. Run `GOWORK=off go test -tags=integration ./examples/list-applications/flow -run TestListApplicationsUsesDefaultMockWithRealDex -count=1` with a local `dexcli dev` server for real Dex mock integration.
