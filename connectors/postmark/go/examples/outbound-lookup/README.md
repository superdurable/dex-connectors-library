# Postmark outbound-lookup example

This runnable Worker starts `PostmarkOutboundLookup` with typed `outboundlookup.Request`, calls
`postmark.NewListOutboundMessagesStep`, and completes with `postmark.MessagePage`. The
static Dex Web connection name is `postmark`. Optional failure branches are
unwired and fail the Flow when selected.

The completed provider result is available in the Dex Web summary and display views.

## Generate the strict FDG 2.0 definition

From `connectors/postmark` with a current Dex CLI:

```bash
mkdir -p build
dexcli visualize ./examples/outbound-lookup/flow/workflow.go --schema-version 2.0 --json --out ./build/outbound-lookup
```

Check that the command reports `valid: true`. A local source may warn that a
connector release is required until this module is published.

## Open the unreleased connector in Dex Web

Until this connector has a published release, generate local release metadata
and pass it to the Dex CLI. From the repository root:

```bash
mkdir -p /tmp/postmark-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/postmark/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/postmark/go \
  --version v0.2.0 --tag connectors/postmark/go/v0.2.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/postmark-release/connector-release.json \
  --digest-output /tmp/postmark-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/postmark/go/build" \
  --connector-release-override postmark=/tmp/postmark-release
```

## Configure and run

Create the `postmark` connection in Dex Web. Start at [https://account.postmarkapp.com/servers](https://account.postmarkapp.com/servers),
follow **Servers > select a server > API Tokens**, and paste the secret token in `api_key`.
Leave the displayed configuration defaults blank. The token is never a Flow input.

From this module, with the Dex project environment configured, run:

```bash
go run ./examples/outbound-lookup
```

Start `PostmarkOutboundLookup` in Dex Web with a JSON input matching `postmark.ListOutboundMessagesInput`.
The mock backed real Dex test is `flow/workflow_mock_integration_test.go` and
CI runs it with the integration tag.
