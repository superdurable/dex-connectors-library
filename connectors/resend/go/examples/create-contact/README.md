# Resend create-contact example

This Flow creates a global contact using `resend.NewCreateContactStep` and
completes with the Resend contact ID. It uses the static Dex Web connection
name `resend`. Supply a unique email address and desired first name, last
name, and subscription state. A full-access Resend API key is required.

From the connector directory, validate its Flow Definition Graph 2.0 with
`dexcli visualize go/examples/create-contact/flow/workflow.go --schema-version
2.0 --lint app --json --out /tmp/resend-create-contact-fdg`. Start Dex with
`dexcli dev -open=false -dex-port 9100 -web-port 9101`, use the printed Flow
service address for `DEX_FLOW_SERVICE_ADDRESS`, then run `GOWORK=off go test
-tags=integration ./examples/create-contact/flow -run
'^TestMockBackedFlowWithRealDex$' -count=1 -v` from `go/`. Stop Dex afterward.

To use the real provider, authorize a full-access key in Dex Web and start
`ResendCreateContact` with:

```json
{"email":"reader@example.test","firstName":"Ada","lastName":"Lovelace","unsubscribed":true}
```

Live provider creation and read-back remain unverified without credentials.
