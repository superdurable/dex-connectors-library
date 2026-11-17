# Sentry create-release example

This runnable Flow calls `sentry.NewCreateReleaseStep` with static Dex Web
connection name `sentry`. Start `SentryCreateRelease` with:

```json
{"organization":"acme","project":"app","version":"app@1.0.0"}
```

Find organization and project slugs in Sentry **Settings > Projects**.
The release version is a non-secret identifier unique within the organization.
The token needs a Sentry release creation scope such as `project:releases`.

From the connector directory, run `dexcli visualize go/examples/create-release/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/sentry-create-release-fdg`. Start Dex with `dexcli dev -open=false -dex-port 9100 -web-port 9101`, set `DEX_FLOW_SERVICE_ADDRESS` to the printed address, then run `GOWORK=off go test -tags=integration ./examples/create-release/flow -run '^TestMockBackedFlowWithRealDex$' -count=1 -v` from `go/`.

The mock test verifies the Dex Worker, Client, persistence, and generated Step.
Live Sentry authorization and release creation remain unverified.
