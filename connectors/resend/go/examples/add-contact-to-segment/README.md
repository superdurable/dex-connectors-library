# Resend add-contact-to-segment example

This Flow calls `resend.NewAddContactToSegmentStep` with the static Dex Web
connection name `resend`. Supply a contact ID or email from
[Resend Contacts](https://resend.com/contacts) and a segment ID from
[Resend Segments](https://resend.com/segments). Use a full-access API key
from [Resend API Keys](https://resend.com/api-keys).

From the connector directory, run `dexcli visualize
go/examples/add-contact-to-segment/flow/workflow.go --schema-version 2.0
--lint app --json --out /tmp/resend-add-contact-to-segment-fdg`. Start
`dexcli dev -open=false -dex-port 9100 -web-port 9101`, pass its printed
Flow service address as `DEX_FLOW_SERVICE_ADDRESS`, then run
`GOWORK=off go test -tags=integration ./examples/add-contact-to-segment/flow
-run '^TestMockBackedFlowWithRealDex$' -count=1 -v` from `go/`. Stop Dex
afterward. Live provider authorization remains unverified.
