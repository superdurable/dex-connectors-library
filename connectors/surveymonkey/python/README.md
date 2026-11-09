# SurveyMonkey Python connector

The Python package implements `listSurveys` and `getResponse` with requests and branch decisions replayed against the Go reference. The response output excludes answers and respondent data. Create a private app in [SurveyMonkey Developer Portal](https://developer.surveymonkey.com), open My Apps > the app > Settings, and store its Access Token in the Dex connection secret. Use the API v3 base URL for the account region. See [the connector README](../README.md) and [the runnable example](examples/survey_inspection/README.md).
