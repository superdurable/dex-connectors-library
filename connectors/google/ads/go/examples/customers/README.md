# Google Ads list Flow

This runnable Go Worker registers customer and campaign listing Flows through the `google-ads` Dex Web connection. Supply `ListAccessibleCustomersInput{MaxCustomers: 2}` to bound the customer list retained in Flow state; zero uses 1,000. `GoogleAdsCampaignsList` accepts `ListCampaignsInput`, such as `{"customerId":"1234567890"}`, and stores the returned page and `nextPageToken` in an Attribute. Both results appear in Dex Web summary and display RPCs.

From the connector's `go/` directory run `GOWORK=off go test ./...`. Generate and lint FDG 2.0 with `dexcli visualize examples/customers/flow/campaigns.go --schema-version 2.0 --lint app --json --out /tmp/google-ads-campaign-fdg` from `go/`. With `dexcli dev -open=false -dex-port 9410 -web-port 9411` running, set `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:9410` and run `GOWORK=off go test -tags=integration ./examples/customers/flow -run '^TestCustomersUsesDefaultMockWithRealDex$'` to exercise both checked-in mocks through a real Worker and Client.

With the Google Ads OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/customers` to start the Worker. Live Google Ads authorization has not been exercised here.
