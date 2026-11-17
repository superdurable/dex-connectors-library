# Resend create-automation example

This Flow calls `resend.NewCreateAutomationStep` with the static Dex Web
connection name `resend`. Input supplies a name, status, graph steps, and
connections. The example creates a disabled graph with a trigger and delay;
the provider requires at least one trigger step. Keep secrets out of step
configuration. Manage automations at [Resend Automations](https://resend.com/automations)
and create a full-access API key at [Resend API Keys](https://resend.com/api-keys).

From the connector directory run `dexcli visualize
go/examples/create-automation/flow/workflow.go --schema-version 2.0 --lint app
--json --out /tmp/resend-create-automation-fdg`. Start `dexcli dev -open=false
-dex-port 9100 -web-port 9101`, pass its Flow service address as
`DEX_FLOW_SERVICE_ADDRESS`, then run `GOWORK=off go test -tags=integration
./examples/create-automation/flow -run '^TestMockBackedFlowWithRealDex$' -count=1 -v`
from `go/`. Stop Dex afterward. Live provider authorization is unverified.
