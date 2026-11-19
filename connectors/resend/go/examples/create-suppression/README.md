# Resend create-suppression example

This Flow calls `resend.NewCreateSuppressionStep` with the static Dex Web
connection name `resend`. Input `email` is the address to block from future
delivery. Find existing suppressions at [Resend Suppressions](https://resend.com/suppressions)
and create a full-access API key at [Resend API Keys](https://resend.com/api-keys).

From the connector directory run `dexcli visualize
go/examples/create-suppression/flow/workflow.go --schema-version 2.0 --lint app
--json --out /tmp/resend-create-suppression-fdg`. Start `dexcli dev -open=false
-dex-port 9100 -web-port 9101`, pass its Flow service address as
`DEX_FLOW_SERVICE_ADDRESS`, then run `GOWORK=off go test -tags=integration
./examples/create-suppression/flow -run '^TestMockBackedFlowWithRealDex$' -count=1 -v`
from `go/`. Stop Dex afterward. Live provider authorization is unverified.
