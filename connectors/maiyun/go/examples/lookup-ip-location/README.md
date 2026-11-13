# Look up an MAIYUN IP location

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add an `maiyun` connection in Dex Web with the token from [MAIYUN](https://lts.maiyun.net/) > 控制台 > Token. Start `MAIYUNLocationLookup` with `{"ipAddress":"8.8.8.8"}`. The default mock returns a synthetic Shenzhen result with country, province, city, and district.

Generate a strict FDG 2.0 graph with `dexcli visualize flow/workflow.go --schema-version 2.0 --lint app --json --out flow.fdg.json`. Run `go test ./...` for deterministic provider and Flow tests. With a running local Dex stack, run `go test -tags=integration ./examples/lookup-ip-location/flow -run TestLookupIPLocationUsesDefaultMockWithRealDex`; this starts the real Worker and Client against the synthetic mock. It does not verify an MAIYUN account or consume provider quota.
