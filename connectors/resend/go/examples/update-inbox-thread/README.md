# Resend update-inbox-thread example

This Flow calls `resend.NewUpdateInboxThreadStep` with static Dex Web connection `resend`. Supply `inboxId` and `threadId` from [Resend Inboxes](https://resend.com/inboxes), then supply a `read` value or a `folder` of inbox, archive, spam, or trash. Create a full-access API key at [Resend API Keys](https://resend.com/api-keys).

From the connector directory run `dexcli visualize go/examples/update-inbox-thread/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/resend-update-inbox-thread-fdg`. Start `dexcli dev -open=false -dex-port 9100 -web-port 9101`, pass its Flow service address as `DEX_FLOW_SERVICE_ADDRESS`, then run `GOWORK=off go test -tags=integration ./examples/update-inbox-thread/flow -run '^TestMockBackedFlowWithRealDex$' -count=1 -v` from `go/`. Stop Dex afterward. Live provider authorization is unverified.
