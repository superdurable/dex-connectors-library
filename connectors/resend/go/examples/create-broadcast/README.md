# Resend create-broadcast example

This Flow calls `resend.NewCreateBroadcastStep` with the static Dex Web
connection name `resend`. It creates a draft only. Supply a unique draft
name, a target segment ID from [Resend Segments](https://resend.com/segments),
a verified sender, a subject, and an HTML or text body. Use a full-access
API key from [Resend API Keys](https://resend.com/api-keys); find broadcast IDs
under [Resend Broadcasts](https://resend.com/broadcasts).

From the connector directory, run `dexcli visualize
go/examples/create-broadcast/flow/workflow.go --schema-version 2.0 --lint app
--json --out /tmp/resend-create-broadcast-fdg`. Start `dexcli dev -open=false
-dex-port 9100 -web-port 9101`, pass its printed Flow service address as
`DEX_FLOW_SERVICE_ADDRESS`, then run `GOWORK=off go test -tags=integration
./examples/create-broadcast/flow -run '^TestMockBackedFlowWithRealDex$' -count=1 -v`
from `go/`. Stop Dex afterward. Live provider authorization remains unverified.
