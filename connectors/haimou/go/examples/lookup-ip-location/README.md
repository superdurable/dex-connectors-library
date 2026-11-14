# Look up an HAIMOU IP location

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add an `haimou` connection in Dex Web with the apikey from [亥牟数据](https://hmkj4399.com/docs.html) > 控制台 > 应用管理. Start `HAIMOULocationLookup` with `{"ipAddress":"8.8.8.8"}`. The default mock returns a synthetic Shenzhen result with country, province, city, district, and ISP.

Generate a strict FDG 2.0 graph with `dexcli visualize flow/workflow.go --schema-version 2.0 --lint app --json --out flow.fdg.json`. Run `go test ./...` for deterministic provider and Flow tests. With a running local Dex stack, run `go test -tags=integration ./examples/lookup-ip-location/flow -run TestLookupIPLocationUsesDefaultMockWithRealDex`; this starts the real Worker and Client against the synthetic mock. It does not verify an HAIMOU account or consume provider quota.
