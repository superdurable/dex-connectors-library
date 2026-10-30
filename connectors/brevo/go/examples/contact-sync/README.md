# Brevo contact sync example

This runnable Flow accepts an email and first name in Dex Web Start Flow, validates them, and calls `brevo.NewCreateOrUpdateContactStep`. The happy `saved` branch completes with the contact confirmation. Other branches remain unwired and fail the Flow. `ConnectionName` is statically `brevo`. Dex Web summary and display views show the completed provider outcome.

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
  --version v0.1.0 --tag connectors/brevo/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/brevo-release/connector-release.json \
  --digest-output /tmp/brevo-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/brevo/build" --connector-release-override brevo=/tmp/brevo-release
```

In another terminal, configure the project environment described by [`projectconfig`](../../../sdkgo/projectconfig/README.md) and start the Worker:

```bash
cd connectors/brevo
go run ./examples/contact-sync
```

In Dex Web Connections, configure `brevo / brevo` using the [Brevo API key page](https://app.brevo.com/settings/keys/api). In Start Flow, select `BrevoContactSync`, enter a unique Flow ID, and use:

```json
{"email":"member@your-domain.example","firstName":"Meridian"}
```

Use a real email address and a contact you are allowed to create or update when testing live. The example stores no credential in Flow input or output.

## Tests

`GOWORK=off go test ./...` checks deterministic mapping. CI runs `workflow_mock_integration_test.go` against a real Dex stack with generated Brevo mocks. Live Brevo authorization and provider behavior remain unverified.
