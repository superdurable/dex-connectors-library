# Sentry issue-review example

This runnable Worker starts `SentryIssueReview` with typed `issuereview.Request`, calls
`sentry.NewListIssuesStep`, and completes with `sentry.IssuePage`. The
static Dex Web connection name is `sentry`. Optional failure branches are
unwired and fail the Flow when selected.

## Generate the strict FDG 2.0 definition

From `connectors/sentry` with a current Dex CLI:

```bash
mkdir -p build
dexcli visualize ./examples/issue-review/flow/workflow.go --schema-version 2.0 --json --out ./build/issue-review
```

Check that the command reports `valid: true`. A local source may warn that a
connector release is required until this module is published.

## Open the unreleased connector in Dex Web

Until this connector has a published release, generate local release metadata
and pass it to the Dex CLI. From the repository root:

```bash
mkdir -p /tmp/sentry-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/sentry/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/sentry \
  --version v0.1.0 --tag connectors/sentry/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/sentry-release/connector-release.json \
  --digest-output /tmp/sentry-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/sentry/build" \
  --connector-release-override sentry=/tmp/sentry-release
```

## Configure and run

Create the `sentry` connection in Dex Web. Start at [https://sentry.io/settings/account/api/auth-tokens/](https://sentry.io/settings/account/api/auth-tokens/),
follow **User Settings > Auth Tokens > Create New Token**, and paste the secret token in `api_key`.
Leave the displayed configuration defaults blank. The token is never a Flow input.

From this module, with the Dex project environment configured, run:

```bash
go run ./examples/issue-review
```

Start `SentryIssueReview` in Dex Web with a JSON input matching `sentry.ListIssuesInput`.
The mock backed real Dex test is `flow/workflow_mock_integration_test.go` and
CI runs it with the integration tag.
