# Resend create-event example

This Flow calls `resend.NewCreateEventStep` with the static Dex Web connection
name `resend`. Input `name` defines a reusable automation event type and
`schema` maps optional payload keys to `string`, `number`, `boolean`, or `date`.
Manage event types at [Resend Events](https://resend.com/events) and create a
full-access API key at [Resend API Keys](https://resend.com/api-keys).

From the connector directory run `dexcli visualize
go/examples/create-event/flow/workflow.go --schema-version 2.0 --lint app
--json --out /tmp/resend-create-event-fdg`. Start `dexcli dev -open=false
-dex-port 9100 -web-port 9101`, pass its Flow service address as
`DEX_FLOW_SERVICE_ADDRESS`, then run `GOWORK=off go test -tags=integration
./examples/create-event/flow -run '^TestMockBackedFlowWithRealDex$' -count=1 -v`
from `go/`. Stop Dex afterward. Live provider authorization is unverified.
