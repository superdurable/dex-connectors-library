# WhatsApp Business provider operations

Configure the static `whatsapp-business` connection in Dex Web, then run `npm run example:worker`. The Worker registers `WhatsAppBusinessPhoneNumbers`, `WhatsAppBusinessTextMessage`, and `WhatsAppBusinessTemplateMessage`.

Generate strict FDG 2.0 for each Flow:

```bash
dexcli visualize examples/provider-operations/phone-numbers-flow.ts --schema-version 2.0 --json --out /tmp/whatsapp-typescript-phone-numbers-fdg --lint app
dexcli visualize examples/provider-operations/text-message-flow.ts --schema-version 2.0 --json --out /tmp/whatsapp-typescript-text-message-fdg --lint app
dexcli visualize examples/provider-operations/template-message-flow.ts --schema-version 2.0 --json --out /tmp/whatsapp-typescript-template-message-fdg --lint app
```

Use the numeric WABA ID for the read Flow. For sends, use the sender phone-number ID returned by the read Flow and a recipient in E.164 digits without `+`. Text sends require an open customer service window. Template sends require an approved template name and language code.
