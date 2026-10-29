# Mailgun connector

Read Mailgun domain events and account domains. The manifest is the source of generated Config, Credentials, Connection, operation factories, and mocks. See the [runnable demo](examples/demo/README.md) for Worker and strict FDG 2.0 generation commands.

## Operations and provider references

| Operation | Dex kind | Official API reference |
| --- | --- | --- |
| `getEvents` | Query | https://documentation.mailgun.com/docs/mailgun/api-reference/send/mailgun/events/get-v3-domain_name-events |
| `listDomains` | Query | https://documentation.mailgun.com/docs/mailgun/api-reference/send/mailgun/domains/get-v4-domains |

The demo uses `mailgun.NewGetEventsStep` with the static `mailgun` connection. Each operation is an operation-specific Query Step with the default one-hour retry policy. `getEvents` returns one bounded page; the provider's next-page URL is not exposed until safe page continuation is supported.

## Authorization and configuration

At [Mailgun > Settings > API Security > API Keys](https://app.mailgun.com/settings/api_security), create or select an API key for these operations, copy its provider-issued value, and store it in the secret `api_key` field in Dex Web Connections. A blank key is invalid. The manifest supplies the public base URL, HTTP timeout, and response-size limit; leave their fields blank unless a compatible gateway, slow request, or known larger response requires an override. For domains created in the EU region, set `baseUrl` to `https://api.eu.mailgun.net`.

## Failure behavior

| Condition | Outcome |
| --- | --- |
| 200 with valid bounded output | Happy-path branch |
| 400, 401, 403, or another definite provider rejection | Optional `providerRejected` branch |
| 429, 408, transport error, or 5xx | Retry with the same input until the retry budget ends |
| Malformed or oversized successful response | Optional `invalidResponse` branch |
| Invalid local input or credentials | Optional `defect` branch |

Provider error bodies and secrets are never copied into failures, receipts, or output. Optional branches are unwired in the demo and fail the Flow when selected.

## Mocks

The generated `mailgunmock` package uses one consistent default answer per operation:

- `getEvents` default `listedDefault`: `{"events":[{"id":"event_meridian","event":"delivered","timestamp":1793100000}]}`
- `listDomains` default `listedDefault`: `{"domains":[{"name":"example.test","state":"active"}],"totalCount":1}`

`examples/demo/flow/workflow_mock_integration_test.go` starts a real Dex Flow through the mock connection in CI. Provider unit tests use local HTTP servers for the documented request shape and a rate-limit response.

## Unverified live behavior

A real account key, US/EU base URL choice, read permissions, and Dex Web connection form have not been verified. `sendMessage` is omitted because the provider documentation reviewed does not establish a safe replay/deduplication guarantee. The read operations use HTTP Basic username `api` with the key as the password.
