# SurveyMonkey Python survey inspection

This runnable Flow calls `listSurveys` using the static `surveymonkey` connection, stores the page in an Attribute, and completes with the typed result. Create a private app at https://developer.surveymonkey.com, open My Apps > the app > Settings, and configure its Access Token as a secret in Dex Web Connections. Set the API v3 base URL for EU or Canadian accounts; blank uses the displayed US default.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from `connectors/surveymonkey/python`:

```bash
uv run python -m examples.survey_inspection.worker
```

Start Flow type `SurveyMonkeySurveyInspection` with a unique Flow ID and input `{"page":1,"perPage":50}`. The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected.
