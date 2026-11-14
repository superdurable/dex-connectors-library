# List Jushuitan shops

Run `go run .` from this directory with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `jushuitan` connection in Dex Web using an approved App Key, App Secret, and Access Token. Start `JushuitanShopList` with `{"pageIndex":1,"pageSize":10}`; it returns one page of enabled ERP shops. The example's default mock returns two shops without calling Jushuitan.

Generate and lint a strict FDG 2.0 graph from the runnable source:

```sh
dexcli visualize -json -schema-version 2.0 -lint app -out - flow/workflow.go
```

Use `GOWORK=off go test ./...` for deterministic checks. Run `GOWORK=off go test -tags=integration ./examples/list-shops/flow -run TestListShopsUsesDefaultMockWithRealDex -count=1` with a local `dexcli dev` server for real Dex mock integration.
