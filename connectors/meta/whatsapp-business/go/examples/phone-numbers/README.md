# WhatsApp Business phone-number inspection Flow

This runnable Flow reads one page of phone-number metadata for a WhatsApp Business Account through the static `whatsapp-business` connection. It saves the typed page in an Attribute. Optional branches are unwired and fail the Flow when selected.

From the repository root, generate and check the FDG 2.0 graph:

```bash
dexcli visualize connectors/meta/whatsapp-business/go/examples/phone-numbers/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/whatsapp-business-phone-numbers-fdg --open=false
```

After configuring `whatsapp-business / whatsapp-business` in Dex Web as described in the [connector README](../../README.md), load a Dex project and run `GOWORK=off go run ./examples/phone-numbers` from `connectors/meta/whatsapp-business/go`. Start `WhatsAppBusinessPhoneNumbers` with a unique Flow ID and `{"businessAccountId":"123456789"}`. The Worker requires only an outbound Dex connection.

For a mock-backed real Dex check, start `dexcli dev` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8981 GOWORK=off go test -tags=integration ./examples/phone-numbers/flow -run TestPhoneNumbersUsesDefaultMockWithRealDex` from the Go module. Live Meta authorization remains unverified.
