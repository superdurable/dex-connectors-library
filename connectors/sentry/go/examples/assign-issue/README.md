# Sentry assign-issue example

This runnable Flow sets an issue's assignee through `sentry.NewAssignIssueStep`
and completes with the updated issue. It uses the static Dex Web connection
name `sentry`. Find the organization slug in the Sentry URL, the issue ID on
the **Issues** page, and a user ID or `team:<team ID>` in **Settings > Teams**.
All three values are required. The auth token needs `event:write`.

From the connector directory, run `dexcli visualize go/examples/assign-issue/flow/workflow.go
--schema-version 2.0 --lint app --json --out /tmp/sentry-assign-fdg` to validate
the Flow Definition Graph 2.0. Start Dex with `dexcli dev -open=false
-dex-port 9100 -web-port 9101`, set `DEX_FLOW_SERVICE_ADDRESS` to the printed
Flow service address, and run `GOWORK=off go test -tags=integration
./examples/assign-issue/flow -run '^TestMockBackedFlowWithRealDex$' -count=1 -v`
from `go/` for deterministic mock verification. Stop the Dex process afterward.

To use the real provider, authorize Sentry in Dex Web and start
`SentryAssignIssue` with input such as:

```json
{"organization":"acme","id":"42","assignedTo":"team:7"}
```

Live Sentry authorization and assignment have not been verified without
provider credentials.
