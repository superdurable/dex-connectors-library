# Resend verify-domain example

This Flow calls `resend.NewVerifyDomainStep` with the static Dex Web connection
name `resend`. Its output is the domain ID accepted for asynchronous DNS
verification, not a verified status. Find the ID under
[Resend Domains](https://resend.com/domains), after adding the required DNS
records. Use a full-access API key.

From the connector directory, run `dexcli visualize
go/examples/verify-domain/flow/workflow.go --schema-version 2.0 --lint app
--json --out /tmp/resend-verify-domain-fdg`. Start `dexcli dev -open=false
-dex-port 9100 -web-port 9101`, pass its printed Flow service address as
`DEX_FLOW_SERVICE_ADDRESS`, then run `GOWORK=off go test -tags=integration
./examples/verify-domain/flow -run '^TestMockBackedFlowWithRealDex$' -count=1 -v`
from `go/`. Stop Dex afterward. Live DNS verification remains unverified.
