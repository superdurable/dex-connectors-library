# Send one email from a Dex Flow

`EmailSendMessage` accepts `{ "to": ["jane@example.com"], "subject": "Hello", "text": "Hello Jane" }`. Its generated `sendMessage` Step uses the static `email` connection. The `sent` branch completes with the provider Message-ID. An optional rejection, uncertainty, or defect branch is unwired and fails the Flow for an operator to inspect. An uncertain SMTP result is never sent again automatically.

From `connectors/superdurable/email/typescript`, run `npm ci && npm run build`. Configure the `email` connection in Dex Web using the provider IMAP and SMTP settings and app password described in the [connector README](../../README.md#connection-setup). Start `dexcli dev`, then run `npx tsx examples/send-message/main.ts`. `DEX_FLOW_SERVICE_ADDRESS` selects a nondefault Dex gRPC address. Start `EmailSendMessage` from Dex Web with the JSON input above.

For a credential-free Dex Worker and Client check, run `DEX_FLOW_SERVICE_ADDRESS=<host:port> npx tsx examples/send-message/integration.ts` against a running `dexcli dev`. It exercises the same Flow with generated mock outcomes for acceptance and an unwired rejection.

Generate strict FDG 2.0 from this directory with `dexcli visualize examples/send-message/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/email-send-message-graph`. The Step has one `dex:explanation` alongside its group in the Flow source. The generated `dex-connector.json` supplies connector Step metadata. With the currently installed Dex CLI, strict visualization reports `dynamic_class` and `dynamic_step_target` for the generated factory target; the real Dex Worker and Client integration succeeds.
