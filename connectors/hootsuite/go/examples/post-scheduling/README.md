# Hootsuite post scheduling Flow

This runnable Flow calls `schedulePost` through the static `hootsuite` connection. It stores the typed result in a registered Attribute. Optional branches are unwired and fail the Flow when selected.

From the repository root, check the FDG 2.0 graph:

```sh
dexcli visualize connectors/hootsuite/go/examples/post-scheduling/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/hootsuite-post-scheduling-fdg --open=false
```

Configure `hootsuite / hootsuite` in Dex Web using the [connector guide](../../../README.md). From the Go module, run `GOWORK=off go run ./examples/post-scheduling`. Start `HootsuitePostScheduling` with a unique Flow ID and `{"text":"A useful update","socialProfileId":"115185509","scheduledSendTime":"2026-11-15T14:00:00Z"}` as input, replacing the profile ID and time with a user-approved destination and future UTC instant. This operation schedules a real post when using a live connection. If the outcome is uncertain, inspect Hootsuite Planner before resending.

For a mock backed real Dex check, start `dexcli dev` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8841 GOWORK=off go test -tags=integration ./examples/post-scheduling/flow -run TestPostSchedulingUsesDefaultMockWithRealDex`. Live Hootsuite authorization and provider calls require an approved developer app and remain unverified.
