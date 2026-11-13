# List Mingdao HAP worksheets

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `mingdao` connection in Dex Web using the application's AppKey and Sign from its API authorization page. Start `MingdaoWorksheets` with `{}`. The default mock returns one worksheet without calling the provider.

Generate and lint strict FDG 2.0 from the runnable source:

```sh
dexcli visualize -json -schema-version 2.0 -lint app -out flow/flow.fdg flow/workflow.go
```

Use `GOWORK=off go test ./...` for deterministic checks. Run `GOWORK=off go test -tags=integration ./examples/list-worksheets/flow -run TestListWorksheetsUsesDefaultMockWithRealDex -count=1` with a local `dexcli dev` server for real Dex mock integration.
