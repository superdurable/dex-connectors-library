# SurveyMonkey survey-inspection Flow

This runnable Flow calls `listSurveys` with the static `surveymonkey` connection. Its happy-path survey page is saved in an Attribute and shown through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected. No response answers or Access Token enter the Flow result.

## Strict FDG 2.0 generation

From `connectors/surveymonkey/go`, run:

```bash
dexcli visualize ./examples/survey-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/surveymonkey-survey-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override surveymonkey=<artifact-directory>`. In Dex Web Connections select `surveymonkey / surveymonkey`, create a private app at https://developer.surveymonkey.com, open My Apps > the app > Settings, and save its Access Token in the secret field. Set `baseUrl` for EU or Canadian accounts; US accounts can leave it blank for the displayed default.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/surveymonkey/go`:

```bash
GOWORK=off go run ./examples/survey-inspection
```

Start `SurveyMonkeySurveyInspection` with a unique Flow ID and input `{"page":1,"perPage":50}`. The Worker connects to Dex Server without an inbound listener. The provider connection name is fixed in `flow/workflow.go`.

With `dexcli dev` serving at `127.0.0.1:8905`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8905 GOWORK=off go test -race -tags=integration ./examples/survey-inspection/flow -count=1`. Live provider authorization remains a separate check.
