# Sentry create-team example

This runnable Flow calls `sentry.NewCreateTeamStep` with static Dex Web
connection name `sentry`. Start `SentryCreateTeam` with:

```json
{"organization":"acme","slug":"core","name":"Core"}
```

Find the organization slug in Sentry **Settings > General Settings**.
Choose a team slug that is unique within that organization. The token needs
`team:write`, `org:write`, or `org:admin` for creation; `team:admin`
is needed for deletion.

From the connector directory, run `dexcli visualize go/examples/create-team/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/sentry-create-team-fdg`. Start Dex with `dexcli dev -open=false -dex-port 9100 -web-port 9101`, set `DEX_FLOW_SERVICE_ADDRESS` to its printed address, then run `GOWORK=off go test -tags=integration ./examples/create-team/flow -run '^TestMockBackedFlowWithRealDex$' -count=1 -v` from `go/`.

The mock test verifies the Dex Worker, Client, persistence, and generated Step.
Live Sentry authorization and team creation remain unverified.
