# Resend create-inbox example

This Flow calls `resend.NewCreateInboxStep` with the static Dex Web connection
name `resend`. Input `emailAddress` must use a verified sending domain;
`name` and `fromName` are optional display names. Set `forwarding` when the
domain does not have inbound MX configured and the team has a Resend inbound
domain. Manage inboxes at [Resend Inboxes](https://resend.com/inboxes) and
create a full-access API key at [Resend API Keys](https://resend.com/api-keys).

From the connector directory run `dexcli visualize
go/examples/create-inbox/flow/workflow.go --schema-version 2.0 --lint app
--json --out /tmp/resend-create-inbox-fdg`. Start `dexcli dev -open=false
-dex-port 9100 -web-port 9101`, pass its Flow service address as
`DEX_FLOW_SERVICE_ADDRESS`, then run `GOWORK=off go test -tags=integration
./examples/create-inbox/flow -run '^TestMockBackedFlowWithRealDex$' -count=1 -v`
from `go/`. Stop Dex afterward. Live provider authorization is unverified.
