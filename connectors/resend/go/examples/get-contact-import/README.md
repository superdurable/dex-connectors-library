# Resend get-contact-import example

This Flow calls `resend.NewGetContactImportStep` with the static Dex Web
connection name `resend`. Supply the import ID shown under
[Resend Contacts](https://resend.com/contacts). Use a full-access API key
from [Resend API Keys](https://resend.com/api-keys).

From the connector directory, run `dexcli visualize
go/examples/get-contact-import/flow/workflow.go --schema-version 2.0
--lint app --json --out /tmp/resend-get-contact-import-fdg`. Start
`dexcli dev -open=false -dex-port 9100 -web-port 9101`, pass its printed
Flow service address as `DEX_FLOW_SERVICE_ADDRESS`, then run
`GOWORK=off go test -tags=integration ./examples/get-contact-import/flow
-run '^TestMockBackedFlowWithRealDex$' -count=1 -v` from `go/`. Stop Dex
afterward. Live provider authorization remains unverified.
