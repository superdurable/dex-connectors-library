# Resend account usage example

This Flow calls `resend.NewGetUsageStep` using the static Dex Web connection
name `resend`. Input `period` is `all`, `daily`, or `monthly`; blank means all.
The result contains current email counts and plan limits. Configure a
full-access API key at [Resend API Keys](https://resend.com/api-keys).

From the connector directory run `dexcli visualize
go/examples/account-usage/flow/workflow.go --schema-version 2.0 --lint app
--json --out /tmp/resend-account-usage-fdg`. Start `dexcli dev -open=false
-dex-port 9100 -web-port 9101`, pass its Flow service address as
`DEX_FLOW_SERVICE_ADDRESS`, then run `GOWORK=off go test -tags=integration
./examples/account-usage/flow -run '^TestMockBackedFlowWithRealDex$' -count=1 -v`
from `go/`. Stop Dex afterward. Live provider authorization is unverified.
