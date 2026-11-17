# Brevo contact sync example

This runnable Flow accepts an email and first name in Dex Web Start Flow, validates them, and calls `brevo.NewCreateOrUpdateContactStep` by default. Set `strictCreate: true` to call `brevo.NewCreateContactStep` and reject an existing email. It then lists contact attribute definitions. If `senderId` and `senderName` are supplied, it calls `brevo.NewUpdateSenderStep`. If `attributeName` and `attributeOptions` are supplied, it calls `brevo.NewUpdateAttributeStep` to replace all options of an existing normal multiple-choice attribute. Optional email fields call `brevo.NewSendEmailStep` or `brevo.NewSendTemplateEmailStep`. The happy branches complete with the contact confirmation. Other branches remain unwired and fail the Flow. `ConnectionName` is statically `brevo`. Dex Web summary and display views show the completed provider outcome.

## Generate strict FDG 2.0

From `connectors/brevo` with the latest stable Dex CLI:

```bash
mkdir -p build
dexcli visualize ./examples/contact-sync/flow/workflow.go --schema-version 2.0 --json --out ./build/contact-sync
```

The result must report `valid: true`. A local connector may report `connector_release_required` until its release is available.

## Run

Create the release override from the repository root:

```bash
mkdir -p /tmp/brevo-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/brevo/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/brevo/go \
  --version v0.3.0 --tag connectors/brevo/go/v0.3.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/brevo-release/connector-release.json \
  --digest-output /tmp/brevo-release/connector-release.json.sha256
dexcli dev -open=false -dex-port 9240 -web-port 9241 --flow-rendering-dir "$PWD/connectors/brevo/go/build" --connector-release-override brevo=/tmp/brevo-release
```

In another terminal, configure the project environment described by [`projectconfig`](../../../sdkgo/projectconfig/README.md) and start the Worker:

```bash
cd connectors/brevo/go
go run ./examples/contact-sync
```

In Dex Web Connections, configure `brevo / brevo` using the [Brevo API key page](https://app.brevo.com/settings/keys/api). In Start Flow, select `BrevoContactSync`, enter a unique Flow ID, and use:

```json
{"email":"member@your-domain.example","firstName":"Meridian"}
```

Use a real email address and a contact you are allowed to create or update when testing live. The example stores no credential in Flow input or output.

To update a sender as part of the same Flow, add `senderId` as a positive numeric ID from `listSenders` and `senderName` as its new display name. Leave both absent to skip the sender update.

To update an attribute, add an uppercase `attributeName` from `listAttributes` and a nonempty array of `attributeOptions` strings, each at most 200 characters. The options replace the complete existing set. Leave both absent to skip the attribute update.

To send an inline email, add `emailSenderAddress` and `emailSenderName` for a verified Brevo sender, `emailSubject`, and `emailText`. To send a configured template instead, sign in at [Brevo](https://app.brevo.com), open **Transactional > Templates**, create or select an active template, and enter the numeric ID shown after `#` as `emailTemplateId`; optional `emailTemplateParams` supplies string variable values. Both send to the contact's `email`. Leave all email fields absent to skip sending. Do not combine inline and template fields.

## Tests

`GOWORK=off go test ./...` checks deterministic mapping. CI runs `workflow_mock_integration_test.go` against a real Dex stack with generated Brevo mocks. Live Brevo authorization and provider behavior remain unverified.
