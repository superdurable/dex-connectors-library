# Sentry create-project example

This Flow creates a project under a Sentry team using
`sentry.NewCreateProjectStep`, then completes with the new project metadata.
It uses the static Dex Web connection `sentry`. Find the organization and
team slugs in Sentry URLs under **Settings > Teams**. Supply a project name
and a stable slug unique within the organization; all fields are required.
The token needs `project:write` or `project:admin`, and some organizations
also require `org:write` or Team Admin permission.

From the connector directory, validate FDG 2.0 with `dexcli visualize
go/examples/create-project/flow/workflow.go --schema-version 2.0 --lint app
--json --out /tmp/sentry-create-project-fdg`. Start Dex with `dexcli dev
-open=false -dex-port 9100 -web-port 9101`, set `DEX_FLOW_SERVICE_ADDRESS` to
the printed address, and run `GOWORK=off go test -tags=integration
./examples/create-project/flow -run '^TestMockBackedFlowWithRealDex$' -count=1
-v` from `go/`. Stop Dex afterward.

To use Sentry itself, authorize in Dex Web and start `SentryCreateProject`:

```json
{"organization":"acme","team":"core","name":"Application","slug":"app"}
```

Live provider creation and read-back remain unverified without credentials.
