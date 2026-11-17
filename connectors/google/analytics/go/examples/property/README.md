# Google Analytics 4 property Flow (Go)

This runnable Worker registers a GA4 property lookup and a realtime report Flow through the static `google-analytics-4` connection. `GoogleAnalytics4Realtime` accepts `RunRealtimeReportInput`, for example `{"propertyId":"123456789","dimensions":["country"],"metrics":["activeUsers"]}`, and stores a bounded report page. See the [connector guide](../../../README.md) for OAuth setup and the numeric Property ID.

From the connector module run `GOWORK=off go test ./...`. Generate the realtime FDG with `dexcli visualize examples/property/flow/realtime.go --schema-version 2.0 --lint app --json --out /tmp/analytics4-realtime-fdg`. With `dexcli dev -open=false -dex-port 9410 -web-port 9411` running, set `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:9410` and run `GOWORK=off go test -tags=integration ./examples/property/flow -run '^TestPropertyUsesDefaultMockWithRealDex$'` to verify both checked-in mocks through a real Worker and Client.

With a Google OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/property` to start the Worker. Live Google authorization has not been verified in this checkout.
