# List Jiandaoyun authorized applications

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `fanruan` connection in Dex Web using a Jiandaoyun API Key. Start `JiandaoyunAppsPage` with `{"skip":0,"limit":100}`. `skip` omits that many earlier apps; `limit` must be 1–100, and zero uses 100. If `nextSkip` is nonzero, request that offset to check for another page. The default mock returns one app.

Generate and lint strict FDG 2.0 from the runnable source:

```sh
dexcli visualize -json -schema-version 2.0 -lint app -out flow/flow.fdg flow/workflow.go
```

Use `GOWORK=off go test ./...` for deterministic checks. Run `GOWORK=off go test -tags=integration ./examples/list-apps-page/flow -run TestListAppsPageUsesDefaultMockWithRealDex -count=1` with a local `dexcli dev` server for real Dex mock integration.
