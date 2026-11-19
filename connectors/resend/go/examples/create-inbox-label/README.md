# Resend create-inbox-label example

This Flow calls `resend.NewCreateInboxLabelStep` with static Dex Web connection `resend`. Supply `inboxId` from [Resend Inboxes](https://resend.com/inboxes) and a label `name`; optional `color` is #RRGGBB. Create a full-access API key at [Resend API Keys](https://resend.com/api-keys).

From the connector directory run `dexcli visualize go/examples/create-inbox-label/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/resend-create-inbox-label-fdg`. Start `dexcli dev -open=false -dex-port 9100 -web-port 9101`, pass its Flow service address as `DEX_FLOW_SERVICE_ADDRESS`, then run `GOWORK=off go test -tags=integration ./examples/create-inbox-label/flow -run '^TestMockBackedFlowWithRealDex$' -count=1 -v` from `go/`. Stop Dex afterward. Live provider authorization is unverified.
