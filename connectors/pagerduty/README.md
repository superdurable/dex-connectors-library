# PagerDuty connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/pagerduty/go@v0.4.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/pagerduty`.

This Dex connector calls the public PagerDuty JSON API. Its
manifest version is `v0.4.0`; the Go module requires connector SDK `v0.38.0`
    and Dex Go SDK `v1.26.0` or newer. The runnable
[`incident-review` example](go/examples/incident-review/README.md) shows a
complete Worker and the generated operation-specific Step factory.

The Python package is [`dex-connector-pagerduty`](python/README.md), imported as
`dex_connectors.pagerduty`. The TypeScript package is
[`@superdurable/connector-pagerduty`](typescript/README.md).

## Operations

| Operation | Kind | Output |
| --- | --- | --- |
| `listIncidents` | query | IncidentPage |
| `getIncident` | query | Incident |
| `listIncidentNotes` | query | IncidentNotes |
| `listLogEntries` | query | LogEntryPage |
| `getLogEntry` | query | LogEntry |
| `getUser` | query | User |
| `updateIncident` | mutation, safe | Incident |
| `createIncident` | mutation, limited (3 sends) | Incident |
| `createIncidentNote` | mutation, limited (3 sends) | IncidentNote |

`listIncidents` uses PagerDuty's default one-month date window unless a later release adds filters. `updateIncident` sets the absolute status `acknowledged` or `resolved`. Incident creation uses the Dex call ID as PagerDuty's incident key and reads back by that key before a possible resend. Note creation has no documented replay key, so it can create a duplicate on a retry; the three-send cap selects `uncertain` when acceptance cannot be confirmed. Events API deduplication does not prove a repeated trigger is safe after incident resolution, so `triggerEvent` is omitted.

## Authorization

Start at [https://support.pagerduty.com/main/docs/api-access-keys](https://support.pagerduty.com/main/docs/api-access-keys) and follow
**Integrations > Developer Tools > API Access Keys**. Create a full-access REST API key for incident and note writes, or a read-only key for queries, and copy it. It is an opaque secret string; blank prevents requests. Paste the token in the
secret `api_key` field in Dex Web Connections.
The key travels only in `Authorization`. Leave `baseUrl`, `timeout`,
and `maxResponseBytes` blank for the displayed public endpoint and safety
defaults; override the base URL only for a compatible gateway or test server.

## Failures and retries

| Condition | Result |
| --- | --- |
| Successful documented response | operation's happy branch |
| Known missing resource on a detail operation | `notFound` |
| Other definite 4xx rejection | `providerRejected` |
| Malformed or oversized successful JSON | `invalidResponse` |
| Invalid local input or credentials | `defect` |
| Query or safe update HTTP 408, 429, 5xx, transport interruption | Retry until the default budget ends, then Flow failure |
| Create HTTP 429 or 503 | Retry after a no-effect checkpoint |
| Create 5xx or transport interruption | Retry within three possible sends, then `uncertain` if read-back cannot confirm |

Every non-happy branch is optional. The example wires only the happy branch;
an unwired optional outcome fails the Flow. Provider error response text and
credentials never enter a Dex result, receipt, Stream, or log.

## Mocks

`pagerdutymock.New(t, "mock")` binds the default manifest answers and the
generated `zz_generated_mock_test.go` strictly decodes every fixture. The
checked-in [`workflow_mock_integration_test.go`](go/examples/incident-review/flow/workflow_mock_integration_test.go)
uses the same generated mock against a real Dex Worker in CI. Its start looks
like this runnable source:

```go
mock := pagerdutymock.New(t, ConnectionName)
flow := NewFlow(mock.Connection())
```

## Provider documentation and verification

- [https://docs.pagerduty.com/developer/api/reference/rest/incidents/list-incidents](https://docs.pagerduty.com/developer/api/reference/rest/incidents/list-incidents)
- [https://docs.pagerduty.com/developer/api/reference/rest/incidents/get-incident](https://docs.pagerduty.com/developer/api/reference/rest/incidents/get-incident)
- [https://docs.pagerduty.com/developer/api/reference/rest/incidents/list-incident-notes](https://docs.pagerduty.com/developer/api/reference/rest/incidents/list-incident-notes)
- [https://docs.pagerduty.com/developer/api/reference/rest/log-entries/list-log-entries](https://docs.pagerduty.com/developer/api/reference/rest/log-entries/list-log-entries)
- [https://docs.pagerduty.com/developer/api/reference/rest/log-entries/get-log-entry](https://docs.pagerduty.com/developer/api/reference/rest/log-entries/get-log-entry)
- [https://docs.pagerduty.com/developer/api/reference/rest/incidents/create-incident](https://docs.pagerduty.com/developer/api/reference/rest/incidents/create-incident)
- [https://docs.pagerduty.com/developer/api/reference/rest/incidents/update-incident](https://docs.pagerduty.com/developer/api/reference/rest/incidents/update-incident)
- [https://docs.pagerduty.com/developer/api/reference/rest/incidents/create-incident-note](https://docs.pagerduty.com/developer/api/reference/rest/incidents/create-incident-note)
- [https://docs.pagerduty.com/developer/authentication](https://docs.pagerduty.com/developer/authentication)

The HTTP unit tests verify request shapes and local classification against
an `httptest` server. Provider authorization, live API response variations,
and real Dex integration remain unverified locally. CI runs the mock backed
real Dex test; a user-controlled live connection is still needed to confirm
the provider account and permissions.
