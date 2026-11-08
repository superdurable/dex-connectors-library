# Google Contacts creation Flow (Go)

This runnable Dex Flow creates one contact through the static `google-contacts` connection. See the [connector guide](../../../README.md) for OAuth setup. Start it with a nonempty `givenName` and optional `familyName` and `emailAddress`; the completed result contains the new `people/{id}` resource name.

From the connector directory, generate and lint FDG 2.0 with `dexcli visualize go/examples/creation/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/google-contacts-creation-fdg --open=false`. From `go/`, run `GOWORK=off go test ./...`. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `GOWORK=off go test -tags=integration ./examples/creation/flow -run TestCreationUsesDefaultMockWithRealDex` for the real Worker and Client path.

With the Google OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/creation` to start the Worker. A confirmed 429 or 503 can retry; an unconfirmed sent request returns `uncertain` to prevent a duplicate contact. Live Google authorization and API behavior have not been verified in this checkout.
