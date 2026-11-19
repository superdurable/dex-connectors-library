# Resend create-segment example

This Flow calls `resend.NewCreateSegmentStep` with the static Dex Web
connection name `resend`. Supply a segment name from the contact
organization you want to create. Use a full-access API key from
[Resend API Keys](https://resend.com/api-keys), and find created segment IDs
under [Resend Segments](https://resend.com/segments).

From the connector directory, run `dexcli visualize
go/examples/create-segment/flow/workflow.go --schema-version 2.0 --lint app
--json --out /tmp/resend-create-segment-fdg`. Start `dexcli dev -open=false
-dex-port 9100 -web-port 9101`, pass its printed Flow service address as
`DEX_FLOW_SERVICE_ADDRESS`, then run `GOWORK=off go test -tags=integration
./examples/create-segment/flow -run '^TestMockBackedFlowWithRealDex$' -count=1 -v`
from `go/`. Stop Dex afterward. Live provider authorization remains unverified.
