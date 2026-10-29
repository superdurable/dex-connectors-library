# SendGrid connector

Read SendGrid global statistics and transactional templates. The manifest is the source of generated Config, Credentials, Connection, operation factories, and mocks. See the [runnable demo](examples/demo/README.md) for Worker and strict FDG 2.0 generation commands.

## Operations and provider references

| Operation | Dex kind | Official API reference |
| --- | --- | --- |
| `getStats` | Query | https://www.twilio.com/docs/sendgrid/api-reference/stats/retrieve-global-email-statistics |
| `listTemplates` | Query | https://www.twilio.com/docs/sendgrid/api-reference/transactional-templates/retrieve-paged-transactional-templates |

The demo uses `sendgrid.NewGetStatsStep` with the static `sendgrid` connection. Each operation is an operation-specific Query Step. Generation operations use synchronous Execute durability and a three-attempt billed retry budget; read operations use the default one-hour retry policy.

## Authorization and configuration

At [SendGrid > Settings > API Keys > Create API Key](https://app.sendgrid.com/settings/api_keys), create or select an API key for these operations, copy its provider-issued value, and store it in the secret `api_key` field in Dex Web Connections. A blank key is invalid. The manifest supplies the public base URL, HTTP timeout, and response-size limit; leave their fields blank unless a compatible gateway, slow request, or known larger response requires an override. For an EU account, supply its documented regional base URL in `baseUrl`.

## Failure behavior

| Condition | Outcome |
| --- | --- |
| 200 with valid bounded output | Happy-path branch |
| 400, 401, 403, or another definite provider rejection | Optional `providerRejected` branch |
| 404 for OpenWeatherMap | Optional `notFound` branch |
| 429, 408, transport error, or 5xx | Retry with the same input until the retry budget ends |
| Malformed or oversized successful response | Optional `invalidResponse` branch |
| Invalid local input or credentials | Optional `defect` branch |

Provider error bodies and secrets are never copied into failures, receipts, or output. Optional branches are unwired in the demo and fail the Flow when selected.

## Mocks

The generated `sendgridmock` package uses one consistent default answer per operation:

- `getStats` default `foundDefault`: `{"days":[{"date":"2026-10-27","requests":8,"delivered":7}]}`
- `listTemplates` default `listedDefault`: `{"templates":[{"id":"d-meridian","name":"Meridian receipt","generation":"dynamic"}],"count":1}`

`examples/demo/flow/workflow_mock_integration_test.go` starts a real Dex Flow through the mock connection in CI. Provider unit tests use local HTTP servers for the documented request shape and a rate-limit response.

## Unverified live behavior

A real account key, regional base URL choice, read permissions, and Dex Web connection form have not been verified. `sendEmail` is omitted because the provider documentation reviewed does not establish a safe replay/deduplication guarantee.
