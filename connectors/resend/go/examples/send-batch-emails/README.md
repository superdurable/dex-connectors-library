# Resend batch email example

This Flow sends two or more messages through `resend.NewSendBatchEmailsStep`
with the static Dex Web connection name `resend`. Each input message supplies
`from`, `to`, `subject`, and `html`; the sender must be a verified Resend domain.
The result lists queued email IDs in the same order as the input messages.

From the connector directory, run `dexcli visualize
go/examples/send-batch-emails/flow/workflow.go --schema-version 2.0 --lint app
--json --out /tmp/resend-send-batch-fdg`. Start `dexcli dev -open=false
-dex-port 9100 -web-port 9101`, pass the printed Flow service address as
`DEX_FLOW_SERVICE_ADDRESS`, then run `GOWORK=off go test -tags=integration
./examples/send-batch-emails/flow -run '^TestMockBackedFlowWithRealDex$'
-count=1 -v` from `go/`. Stop Dex afterward. Live provider sending is unverified.
