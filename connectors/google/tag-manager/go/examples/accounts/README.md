# Google Tag Manager accounts Flow (Go)

This runnable Dex Flow lists GTM account identities and display names through the static `google-tag-manager` connection. See the [connector guide](../../../README.md) for OAuth setup.

From the connector module run `GOWORK=off go test ./...`. Generate the FDG from the connector directory with `dexcli visualize go/examples/accounts/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/tagmanager-accounts-fdg --open=false`. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `GOWORK=off go test -tags=integration ./examples/accounts/flow -run TestAccountsUsesDefaultMockWithRealDex` to verify the checked-in mock through a real Worker and Client.

With a Google OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/accounts` to start the Worker. Live Google authorization has not been verified in this checkout.
