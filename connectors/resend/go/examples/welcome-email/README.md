# Resend welcome-email example

This runnable Worker starts `ResendWelcomeEmail` with typed `welcomeemail.Request`, calls
`resend.NewSendEmailStep`, and completes with `resend.SentEmail`. The
static Dex Web connection name is `resend`. Optional failure branches are
unwired and fail the Flow when selected.

The completed provider result is available in the Dex Web summary and display views.

## Generate the strict FDG 2.0 definition

From `connectors/resend` with a current Dex CLI:

```bash
mkdir -p build
dexcli visualize ./examples/welcome-email/flow/workflow.go --schema-version 2.0 --json --out ./build/welcome-email
```

Check that the command reports `valid: true`. A local source may warn that a
connector release is required until this module is published.

## Open the unreleased connector in Dex Web

Until this connector has a published release, generate local release metadata
and pass it to the Dex CLI. From the repository root:

```bash
mkdir -p /tmp/resend-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/resend/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/resend/go \
  --version v0.1.0 --tag connectors/resend/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/resend-release/connector-release.json \
  --digest-output /tmp/resend-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/resend/build" \
  --connector-release-override resend=/tmp/resend-release
```

## Configure and run

Create the `resend` connection in Dex Web. Start at [https://resend.com/api-keys](https://resend.com/api-keys),
follow **API Keys > Create API Key**, and paste the secret token in `api_key`.
Leave the displayed configuration defaults blank. The token is never a Flow input.

From this module, with the Dex project environment configured, run:

```bash
go run ./examples/welcome-email
```

Start `ResendWelcomeEmail` in Dex Web with a JSON input matching `resend.SendEmailInput`.
The mock backed real Dex test is `flow/workflow_mock_integration_test.go` and
CI runs it with the integration tag.
