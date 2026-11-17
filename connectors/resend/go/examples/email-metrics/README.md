# Resend email-metrics example

This Flow calls `resend.NewGetEmailMetricsStep` with the static Dex Web
connection name `resend`. Its input selects an ISO 8601 date range and
metric names; blank filters use Resend's seven-day account defaults. A
full-access API key from [Resend API Keys](https://resend.com/api-keys)
is required. See [Email Metrics](https://resend.com/docs/api-reference/emails/get-email-metrics)
for available measures and dimensions.

From the connector directory, run `dexcli visualize
go/examples/email-metrics/flow/workflow.go --schema-version 2.0 --lint app
--json --out /tmp/resend-email-metrics-fdg`. Start `dexcli dev -open=false
-dex-port 9100 -web-port 9101`, pass its printed Flow service address as
`DEX_FLOW_SERVICE_ADDRESS`, then run `GOWORK=off go test -tags=integration
./examples/email-metrics/flow -run '^TestMockBackedFlowWithRealDex$' -count=1 -v`
from `go/`. Stop Dex afterward. Live provider authorization remains unverified.
