# Google Analytics 4 property Flow (Go)

This runnable Dex Flow reads one GA4 property's display name, time zone, and currency through the static `google-analytics-4` connection. See the [connector guide](../../../README.md) for OAuth setup and the numeric Property ID.

From the connector module run `GOWORK=off go test ./...`. Generate the FDG from the connector directory with `dexcli visualize go/examples/property/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/analytics4-property-fdg --open=false`. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `GOWORK=off go test -tags=integration ./examples/property/flow -run TestPropertyUsesDefaultMockWithRealDex` to verify the checked-in mock through a real Worker and Client.

With a Google OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/property` to start the Worker. Live Google authorization has not been verified in this checkout.
