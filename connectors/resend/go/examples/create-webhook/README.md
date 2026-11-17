# Resend create-webhook example

This Flow calls `resend.NewCreateWebhookStep` with the static Dex Web
connection name `resend`. Input `endpoint` is a public HTTPS receiver URL
and `events` lists Resend event types. Set up your receiver and find webhook
settings at [Resend Webhooks](https://resend.com/webhooks); create a full-access
API key at [Resend API Keys](https://resend.com/api-keys). The connector result
contains the webhook ID and omits the signing secret. Obtain the secret from
the provider's protected webhook settings when configuring the receiver.

From the connector directory run `dexcli visualize
go/examples/create-webhook/flow/workflow.go --schema-version 2.0 --lint app
--json --out /tmp/resend-create-webhook-fdg`. Start `dexcli dev -open=false
-dex-port 9100 -web-port 9101`, pass its Flow service address as
`DEX_FLOW_SERVICE_ADDRESS`, then run `GOWORK=off go test -tags=integration
./examples/create-webhook/flow -run '^TestMockBackedFlowWithRealDex$' -count=1 -v`
from `go/`. Stop Dex afterward. Live provider authorization is unverified.
