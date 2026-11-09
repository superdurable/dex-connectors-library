# Hootsuite social profiles Flow

This runnable Flow calls `listSocialProfiles` through the static `hootsuite` connection. It stores the typed result in a registered Attribute. Optional branches are unwired and fail the Flow when selected.

From the repository root, check the FDG 2.0 graph:

```sh
dexcli visualize connectors/hootsuite/go/examples/social-profiles/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/hootsuite-social-profiles-fdg --open=false
```

Configure `hootsuite / hootsuite` in Dex Web using the [connector guide](../../../README.md). From the Go module, run `GOWORK=off go run ./examples/social-profiles`. Start `HootsuiteSocialProfiles` with a unique Flow ID and `{}` as input.

For a mock backed real Dex check, start `dexcli dev` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8841 GOWORK=off go test -tags=integration ./examples/social-profiles/flow -run TestSocialProfilesUsesDefaultMockWithRealDex`. Live Hootsuite authorization and provider calls require an approved developer app and remain unverified.
