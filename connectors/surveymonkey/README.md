# SurveyMonkey connector

The Go connector lists survey IDs and titles with `listSurveys` and retrieves response ID, survey ID, status, and timestamps with `getResponse`. Questions, answers, respondent IP addresses, and custom variables are excluded from Flow results. The Access Token is sent in an Authorization Bearer header, never in a URL or result.

Create a private app in [SurveyMonkey Developer Portal](https://developer.surveymonkey.com), open My Apps > the app > Settings, and copy its Access Token into the Dex Web secret field. Public app tokens require View Surveys and View Responses scopes. Use the [v3 API URL](https://api.surveymonkey.com/v3/docs) matching the account's US, EU, or Canadian region. Blank `baseUrl` uses the US default. The [survey-inspection Flow](go/examples/survey-inspection/README.md) is the runnable consumer.

Both reads retry transport failures, HTTP 429, request timeout, and server failures. `getResponse` selects `notFound` on a definitive 404. Provider rejections and malformed or oversized responses select optional branches. An unwired optional branch fails the Flow. Python and TypeScript ports follow Go-verified scenarios after the Go release.
