# Mailgun connector

The Go manifest declares `v0.3.0`; after release, install with `go get github.com/superdurable/dex-connectors-library/connectors/mailgun/go@v0.3.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/mailgun`.

Read Mailgun domain events and account domains, and queue text or HTML email through a verified sending domain. The manifest is the source of generated Config, Credentials, Connection, operation factories, and mocks. See the [runnable demo](go/examples/demo/README.md) for Worker and strict FDG 2.0 generation commands.

The Python package is [`dex-connector-mailgun`](python/README.md), imported as
`dex_connectors.mailgun`.

The TypeScript package is [`@superdurable/connector-mailgun`](typescript/README.md). Its [runnable get events Flow](typescript/examples/get-events/README.md) uses the same static `mailgun` connection.

## Operations and provider references

| Operation | Dex kind | Official API reference |
| --- | --- | --- |
| `getEvents` | Query | https://documentation.mailgun.com/docs/mailgun/api-reference/send/mailgun/events/get-v3-domain_name-events |
| `listDomains` | Query | https://documentation.mailgun.com/docs/mailgun/api-reference/send/mailgun/domains/get-v4-domains |
| `sendMessage` | Mutation, limited repeat | https://documentation.mailgun.com/docs/mailgun/api-reference/send/mailgun/messages/post-v3--domain-name--messages |

The demo uses `mailgun.NewGetEventsStep` and `mailgun.NewSendMessageStep` with the static `mailgun` connection. Queries are safe to retry. `sendMessage` uses a single-dispatch fence and a maximum of three possibly effective sends; Mailgun does not document a deduplication key. An unconfirmed outcome retries below the cap and selects `uncertain` at the cap. `getEvents` returns one bounded page; the provider's next-page URL is not exposed until safe page continuation is supported.

## Authorization and configuration

At [Mailgun > Settings > API Security > API Keys](https://app.mailgun.com/settings/api_security), create or select an API key for these operations, copy its provider-issued value, and store it in the secret `api_key` field in Dex Web Connections. A Domain Sending Key can send for its domain but cannot run the read operations. A blank key is invalid. In Mailgun > Send > Domains, select a verified domain and use its exact DNS name as `domainName`; use an authorized From address. Provide at least one To recipient and either text or HTML. CC, BCC, Reply-To, and analytics tags are optional. The manifest supplies the public base URL, HTTP timeout, and response-size limit; leave their fields blank unless a compatible gateway, slow request, or known larger response requires an override. For domains created in the EU region, set `baseUrl` to `https://api.eu.mailgun.net`.

## Failure behavior

| Condition | Outcome |
| --- | --- |
| 200 with valid bounded output | Happy-path branch |
| 400, 401, 403, or another definite provider rejection | Optional `providerRejected` branch |
| 429, 408, transport error, or 5xx | Retry with the same input until the retry budget ends |
| Malformed or oversized successful response | Optional `invalidResponse` branch |
| Invalid local input or credentials | Optional `defect` branch |

Provider error bodies and secrets are never copied into failures, receipts, or output. Optional branches are unwired in the demo and fail the Flow when selected. The send input supports CC, BCC, Reply-To, tags, and validated custom headers. Binary attachments from the n8n node are omitted because they require a streaming contract.

## Mocks

The generated `mailgunmock` package uses one consistent default answer per operation:

- `getEvents` default `listedDefault`: `{"events":[{"id":"event_meridian","event":"delivered","timestamp":1793100000}]}`
- `listDomains` default `listedDefault`: `{"domains":[{"name":"example.test","state":"active"}],"totalCount":1}`

`examples/demo/flow/workflow_mock_integration_test.go` starts both read and send Flows through the mock connection in CI. Go-recorded provider scenarios cover the multipart send, rejection, 429/503, transport, resend, and uncertain cap. Provider unit tests use local HTTP servers for the read request shapes.

## Unverified live behavior

A real account key, US/EU base URL choice, send/read permissions, and Dex Web connection form have not been verified. A mock-backed Dex Flow does not establish provider delivery. All operations use HTTP Basic username `api` with the key as the password.
