# Resend create-domain-claim example

This Flow calls `resend.NewCreateDomainClaimStep` with static Dex Web connection `resend`. Supply a domain name you own; Resend returns a public TXT challenge to add in DNS. Start at [Resend Domains](https://resend.com/domains), and create a full-access API key at [Resend API Keys](https://resend.com/api-keys). The claim can be verified with `VerifyDomainClaim` after the DNS record propagates.

From the connector directory run `dexcli visualize go/examples/create-domain-claim/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/resend-create-domain-claim-fdg`. Start `dexcli dev -open=false -dex-port 9100 -web-port 9101`, pass its Flow service address as `DEX_FLOW_SERVICE_ADDRESS`, then run `GOWORK=off go test -tags=integration ./examples/create-domain-claim/flow -run '^TestMockBackedFlowWithRealDex$' -count=1 -v` from `go/`. Stop Dex afterward. Live provider authorization is unverified.
