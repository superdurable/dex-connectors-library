# PagerDuty incident-review example

This runnable Worker starts `PagerDutyIncidentReview` with typed `incidentreview.Request`, calls
`pagerduty.NewListIncidentsStep`, and completes with `pagerduty.IncidentPage`. The
static Dex Web connection name is `pagerduty`. Optional failure branches are
unwired and fail the Flow when selected.

The completed provider result is available in the Dex Web summary and display views.

## Generate the strict FDG 2.0 definition

From `connectors/pagerduty` with a current Dex CLI:

```bash
mkdir -p build
dexcli visualize ./examples/incident-review/flow/workflow.go --schema-version 2.0 --json --out ./build/incident-review
```

Check that the command reports `valid: true`. A local source may warn that a
connector release is required until this module is published.

## Open the unreleased connector in Dex Web

Until this connector has a published release, generate local release metadata
and pass it to the Dex CLI. From the repository root:

```bash
mkdir -p /tmp/pagerduty-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/pagerduty/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/pagerduty \
  --version v0.1.0 --tag connectors/pagerduty/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/pagerduty-release/connector-release.json \
  --digest-output /tmp/pagerduty-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/pagerduty/build" \
  --connector-release-override pagerduty=/tmp/pagerduty-release
```

## Configure and run

Create the `pagerduty` connection in Dex Web. Start at [https://support.pagerduty.com/main/docs/api-access-keys](https://support.pagerduty.com/main/docs/api-access-keys),
follow **Integrations > Developer Tools > API Access Keys**, and paste the secret token in `api_key`.
Leave the displayed configuration defaults blank. The token is never a Flow input.

From this module, with the Dex project environment configured, run:

```bash
go run ./examples/incident-review
```

Start `PagerDutyIncidentReview` in Dex Web with a JSON input matching `pagerduty.ListIncidentsInput`.
The mock backed real Dex test is `flow/workflow_mock_integration_test.go` and
CI runs it with the integration tag.
