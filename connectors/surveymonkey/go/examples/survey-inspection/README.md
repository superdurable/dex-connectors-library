# SurveyMonkey survey-inspection Flow

These runnable Flows call `listSurveys` and `getSurvey` with the static `surveymonkey` connection. Happy-path results are saved in Attributes and shown through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected. No response answers or Access Token enter the Flow result.

## Strict FDG 2.0 generation

From `connectors/surveymonkey/go`, run:

```bash
dexcli visualize ./examples/survey-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/surveymonkey-survey-inspection-fdg
dexcli visualize ./examples/survey-inspection/flow/get_survey_flow.go --schema-version 2.0 --json --out /tmp/surveymonkey-get-survey-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact for `connectors/surveymonkey/go/v0.2.0`, then run `dexcli dev -open=false -dex-port 9590 -web-port 9591 --connector-release-override surveymonkey=<artifact-directory>`. In Dex Web Connections select `surveymonkey / surveymonkey`, create a private app at https://developer.surveymonkey.com, open My Apps > the app > Settings, and save its Access Token in the secret field. Set `baseUrl` for EU or Canadian accounts; US accounts can leave it blank for the displayed default.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/surveymonkey/go`:

```bash
GOWORK=off go run ./examples/survey-inspection
```

Start `SurveyMonkeySurveyInspection` with a unique Flow ID and input `{"page":1,"perPage":50}`. The Worker connects to Dex Server without an inbound listener. The provider connection name is fixed in `flow/workflow.go`.

Start `SurveyMonkeyGetSurvey` with a unique Flow ID and input `{"surveyId":"123456789"}` to read one survey with the new operation.

With `dexcli dev` serving at `127.0.0.1:9590`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:9590 GOWORK=off go test -race -tags=integration ./examples/survey-inspection/flow -count=1`. Live provider authorization remains a separate check.
