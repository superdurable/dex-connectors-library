# WhatsApp Business text-message Flow

This runnable Flow calls `sendTextMessage` through the static `whatsapp-business` connection and stores the accepted message ID in an Attribute. Meta accepts freeform text only inside the customer service window; an accepted ID does not prove delivery. Optional provider rejection and uncertainty branches are unwired and fail the Flow.

From the repository root, check the FDG 2.0 graph:

```bash
dexcli visualize connectors/meta/whatsapp-business/go/examples/text-message/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/whatsapp-business-text-message-fdg --open=false
```

Configure `whatsapp-business / whatsapp-business` in Dex Web per the [connector README](../../README.md). From the Go module, run `GOWORK=off go run ./examples/text-message`. Start `WhatsAppBusinessSendTextMessage` with a unique Flow ID and `{"phoneNumberId":"1906385232743451","recipient":"16315551234","text":"Hello"}`. The recipient is an E.164 number without `+`.

For a mock-backed real Dex check, start `dexcli dev` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8981 GOWORK=off go test -tags=integration ./examples/text-message/flow -run TestSendTextMessageUsesDefaultMockWithRealDex` from the Go module. Live Meta authorization remains unverified.
