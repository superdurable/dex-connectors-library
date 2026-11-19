# Resend manage-contact example

This runnable Flow replaces an existing global contact's first name, last name,
and subscription state with `resend.NewUpdateContactStep`. It uses the static
Dex Web connection name `resend`. The contact ID or email comes from the
Resend dashboard's **Contacts** page; the other fields are the desired absolute
values. Blank names clear those fields. The Flow completes with the contact ID.

Run `dexcli visualize go/examples/manage-contact/flow/workflow.go --schema-version
2.0 --lint app --json --out /tmp/resend-contact-fdg` from the connector's Go
module to generate and validate its Flow Definition Graph 2.0.

Start a local Dex stack with `dexcli dev -open=false -dex-port 9100 -web-port
9101`, set `DEX_FLOW_SERVICE_ADDRESS` to the Flow address printed by the CLI,
then run `GOWORK=off go test -tags=integration ./examples/manage-contact/flow
-run '^TestMockBackedFlowWithRealDex$' -count=1 -v` for the deterministic mock
path. Stop the Dex process after the test. For a live Resend run, authorize a
full-access key in Dex Web and start `ResendManageContact` with input such as:

```json
{"idOrEmail":"reader@example.test","firstName":"Ada","lastName":"Lovelace","unsubscribed":true}
```

The live provider path needs an existing contact and has not been verified
without provider credentials.
