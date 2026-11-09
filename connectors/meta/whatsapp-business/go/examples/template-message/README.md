# WhatsApp Business template-message Flow

This runnable Flow calls `sendTemplateMessage` through the static `whatsapp-business` connection and stores the accepted message ID in an Attribute. Create and obtain approval for the named template in WhatsApp Manager before running it. Optional provider rejection and uncertainty branches are unwired and fail the Flow.

From the repository root, check the FDG 2.0 graph:

```bash
dexcli visualize connectors/meta/whatsapp-business/go/examples/template-message/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/whatsapp-business-template-message-fdg --open=false
```

Configure `whatsapp-business / whatsapp-business` in Dex Web per the [connector README](../../README.md). From the Go module, run `GOWORK=off go run ./examples/template-message`. Start `WhatsAppBusinessSendTemplateMessage` with a unique Flow ID and `{"phoneNumberId":"1906385232743451","recipient":"16315551234","templateName":"hello_world","languageCode":"en_US"}`. The recipient is an E.164 number without `+`; `bodyParameters` supplies text placeholders in template order.

For a mock-backed real Dex check, start `dexcli dev` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8981 GOWORK=off go test -tags=integration ./examples/template-message/flow -run TestSendTemplateMessageUsesDefaultMockWithRealDex` from the Go module. Live Meta authorization remains unverified.
