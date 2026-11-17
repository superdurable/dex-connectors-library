# Typeform response recorder

`FormResponseEventFlow` records a signed `form_response` webhook as a typed Dex Attribute. The Worker serves `/webhooks/typeform`, saves each verified delivery in a durable project inbox, and maps `<formId>:<response token>` to one Flow ID. A redelivery starts no second Flow. `responseSubmitted` uses the `formPicker` unit; blank accepts every form whose webhook points at this endpoint.

Start `dexcli dev`, configure the `typeform-workspace` connection in Dex Web, and set `DEX_CONNECTOR_CONFIG_FILE` to its mode-0600 `connections.json`. Generate a Typeform personal token at <https://admin.typeform.com/user/tokens> with `forms:read`, `responses:read`, and `webhooks:write`. Generate a separate `webhook_secret` with `openssl rand -hex 32`. Neither value belongs in Flow input. The manifest authorization guide covers both fields and the API data center.

Run from `connectors/typeform/typescript`:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 WEBHOOK_BIND_ADDRESS=127.0.0.1:8853 npm run example:response
```

Expose `http://127.0.0.1:8853/webhooks/typeform` through an HTTPS tunnel. Use the connector's `upsertWebhook` operation to register that public URL, the form ID, a stable tag, and the connection's signing secret. Typeform must send the `form_response` event. In Dex Web, choose the same form in the binding's **Form** picker; leave it blank to receive every connected form.

The `/readyz` path returns 200 after the Trigger binding starts receiving. The Worker returns 503 before then or if the signing secret is unavailable, so Typeform can retry. A malformed or forged delivery returns 400. The event contains the submitted response with typed answers, field refs and titles, hidden fields, and the submission time. No token or signing secret is persisted in the Flow.

Run the real Dex delivery and restart tests with:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration
```

Generate strict FDG 2.0 from this runnable Flow:

```sh
npm run build
npm install --prefix examples/response-recorder --no-save --no-package-lock --ignore-scripts .
cd examples/response-recorder
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Typeform authorization and delivery have not been verified with an account.
