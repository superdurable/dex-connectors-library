# LinkedIn Ads account listing Flow (Go)

The two runnable Flows read an account page or a campaign-group page through the static `linkedin-ads` Dex Web connection. Supply `ListAdAccountsInput{PageSize: 2}` for an account page or `ListCampaignGroupsInput{AccountID: "507404993", Status: "ACTIVE"}` for campaign groups. Replace the example account ID with one returned by `listAdAccounts`. Pass the returned `NextPageToken` into the next request's `PageToken` to continue. Each result is persisted in a Flow Attribute and exposed through Dex Web summary and display RPCs.

From `go/`, run `GOWORK=off go test ./...`. Generate and lint FDG 2.0 with `dexcli visualize go/examples/accounts/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/linkedin-ads-fdg --open=false` from the connector directory. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `GOWORK=off go test -tags=integration ./examples/accounts/flow -run TestAccountsUsesDefaultMockWithRealDex` to exercise the checked-in mock through a real Worker and Client.

With a LinkedIn Ads OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/accounts` to start the Worker. Live LinkedIn authorization has not been exercised here.
