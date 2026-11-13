# List TDengine Cloud databases

Start `dexcli dev`, then run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to that server and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `taosdata` connection in Dex Web with the HTTPS Cloud URL and Cloud Token from your TDengine Cloud instance's **Programming → REST API** page. Start the `TaosDataListDatabases` Flow with `{}`. Its default mock returns two database records without contacting TDengine Cloud.

Generate and lint a strict FDG 2.0 graph from the runnable source:

```sh
dexcli visualize -json -schema-version 2.0 -lint app -out flow/flow.fdg flow/workflow.go
```

Run `GOWORK=off go test ./...` for deterministic checks. Run `GOWORK=off go test -tags=integration ./examples/list-databases/flow -run TestListDatabasesUsesDefaultMockWithRealDex -count=1` against a local `dexcli dev` server for real Dex mock integration.
