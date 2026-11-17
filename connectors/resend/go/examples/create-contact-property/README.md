# Resend create-contact-property example

This Flow calls `resend.NewCreateContactPropertyStep` with the static Dex Web
connection name `resend`. Supply a property key of at most 50 ASCII letters,
digits, or underscores, a `type` of `string` or `number`, and an optional
fallback value of that type. Use a full-access API key from
[Resend API Keys](https://resend.com/api-keys); find property IDs under
[Resend Contacts](https://resend.com/contacts).

From the connector directory, run `dexcli visualize
go/examples/create-contact-property/flow/workflow.go --schema-version 2.0
--lint app --json --out /tmp/resend-create-contact-property-fdg`. Start
`dexcli dev -open=false -dex-port 9100 -web-port 9101`, pass its printed
Flow service address as `DEX_FLOW_SERVICE_ADDRESS`, then run
`GOWORK=off go test -tags=integration ./examples/create-contact-property/flow
-run '^TestMockBackedFlowWithRealDex$' -count=1 -v` from `go/`. Stop Dex
afterward. Live provider authorization remains unverified.
