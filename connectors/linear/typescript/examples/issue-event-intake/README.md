# Linear Issue event intake

`LinearIssueEventIntakeFlow` starts once for each new Linear issue outside the trash. `issueEventReceived` verifies `Linear-Signature` over the raw body and checks `webhookTimestamp` within the configured one-minute tolerance. The endpoint records each accepted event in the binding's durable project inbox before answering 200; the event ID determines the Flow ID, so a redelivery does not start a second Flow. The Flow persists only the issue ID, team ID, title, action, and event ID. Its optional team picker filters which issues enter the binding.

From `connectors/linear/typescript`, install dependencies with `npm ci`, start `dexcli dev`, set the project's `DEX_PROJECT_*` environment, then run `npm run example:issue-event-intake`. The listener defaults to `127.0.0.1:8838`; override `LINEAR_WEBHOOK_HOST`, `LINEAR_WEBHOOK_PORT`, or `DEX_FLOW_SERVICE_ADDRESS` as needed. Mount `/webhooks/linear` through a public HTTPS URL. `/readyz` answers 200 only while the Trigger binding is receiving.

In Dex Web Connections, create `linear-workspace`, choose the personal API key or OAuth method, and fill `webhook_signing_secret`. A workspace admin creates the webhook at [Linear Settings > API > Webhooks](https://linear.app/settings/api), selects the **Issues** data change event, enters the public HTTPS callback URL, and copies the signing secret from the saved webhook. Configure the `issue-created` binding's **Team** picker; leave it empty for all teams. The example's application admission rule accepts only `create` events outside the trash.

Generate strict FDG 2.0 after building and installing this unpublished connector into the example's own consumer directory:

```sh
npm run build
npm install --prefix examples/issue-event-intake --no-save --no-package-lock --ignore-scripts .
dexcli visualize examples/issue-event-intake/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/dex-linear-ts-trigger-fdg --open=false
```

The generated JSON must report `valid: true`, zero diagnostics, and the `issue-created` binding. For a local real Dex test, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration`. It covers signed delivery, duplicate Flow identity, team filtering, and inbox replay after restarting the application while Dex had been unreachable. No live Linear authorization or webhook was used in this test.
