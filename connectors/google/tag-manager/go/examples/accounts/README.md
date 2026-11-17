# Google Tag Manager accounts Flow (Go)

This runnable Dex example lists GTM account identities and inspects one account through the static `google-tag-manager` connection. The Worker registers both the list Flow and `AccountInspectionFlow`, which uses the operation-specific `getAccount` Step. See the [connector guide](../../../README.md) for OAuth setup.

From the connector module run `GOWORK=off go test ./...`. Generate an FDG from `go/examples/accounts/flow/workflow.go` or `go/examples/accounts/flow/account_inspection.go` with `dexcli visualize <source> --schema-version 2.0 --lint app --json --out /tmp/tagmanager-accounts-fdg`. With `dexcli dev -open=false -dex-port 9440 -web-port 9441` running, set `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:9440` and run `GOWORK=off go test -tags=integration ./examples/accounts/flow -run 'TestAccountsUsesDefaultMockWithRealDex|TestAccountInspectionUsesDefaultMockWithRealDex'` to verify the checked-in mocks through a real Worker and Client.

With a Google OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/accounts` to start the Worker. Live Google authorization has not been verified in this checkout.
