# SurveyMonkey TypeScript connector

This package implements `listSurveys` and `getResponse` for Dex TypeScript Flows. Both operations replay the Go reference's provider requests, branches, retries, and exact outputs through shared conformance scenarios. Response answers and respondent data are excluded from Flow results.

Create a private app in [SurveyMonkey Developer Portal](https://developer.surveymonkey.com), open My Apps > the app > Settings, and copy its Access Token into the Dex Web connection secret. The package sends the token only as `Authorization: Bearer`. Use the API v3 base URL for your account region. The [connector README](../README.md) describes operation inputs and defaults; [survey inspection](examples/survey-inspection/README.md) is the runnable example.

Live SurveyMonkey authorization and responses have not been verified locally. Package release is independent of the Go and Python releases.
