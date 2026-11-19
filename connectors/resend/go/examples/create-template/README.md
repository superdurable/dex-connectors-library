# Resend create-template example

This Flow calls `resend.NewCreateTemplateStep` with the static Dex Web
connection name `resend`. Provide a name and HTML body; optional sender,
subject, alias, text body, reply addresses, and variables can be supplied.
Find existing templates at [Resend Templates](https://resend.com/templates)
and create a full-access API key at [Resend API Keys](https://resend.com/api-keys).

From the connector directory run `dexcli visualize
go/examples/create-template/flow/workflow.go --schema-version 2.0 --lint app
--json --out /tmp/resend-create-template-fdg`. Start `dexcli dev -open=false
-dex-port 9100 -web-port 9101`, pass its Flow service address as
`DEX_FLOW_SERVICE_ADDRESS`, then run `GOWORK=off go test -tags=integration
./examples/create-template/flow -run '^TestMockBackedFlowWithRealDex$' -count=1 -v`
from `go/`. Stop Dex afterward. Live provider authorization is unverified.
