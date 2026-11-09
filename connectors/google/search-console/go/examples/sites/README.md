# Google Search Console sites Flow (Go)

This runnable Dex Flow lists Search Console properties through the static `google-search-console` connection. It stores only property URLs and the connected user's permission levels. See the [connector guide](../../../README.md) for Google OAuth setup.

From the repository root, run `GOWORK=off go run ./cmd/connectorctl generate --check connectors/google/search-console/connector.yaml`. From this module, run `GOWORK=off go test ./...`. With `dexcli dev` running and `DEX_FLOW_SERVICE_ADDRESS` pointing to it, run `GOWORK=off go test -tags=integration ./examples/sites/flow -run TestSitesUsesDefaultMockWithRealDex` to exercise the checked-in mock through a real Worker and Client. Generate the FDG with `dexcli visualize go/examples/sites/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/search-console-sites-fdg --open=false` from the connector directory.

With a Google OAuth connection configured in Dex Web, run `GOWORK=off go run ./examples/sites` to start the Worker. Live Google authorization has not been verified in this checkout.
