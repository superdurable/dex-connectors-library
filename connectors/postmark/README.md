# Postmark connector

This Dex connector calls the public Postmark JSON API. Its
manifest version is `v0.1.0`; the Go module requires connector SDK `v0.33.0`
    and Dex Go SDK `v1.5.0` or newer. The runnable
[`outbound-lookup` example](examples/outbound-lookup/README.md) shows a
complete Worker and the generated operation-specific Step factory.

## Operations

| Operation | Kind | Output |
| --- | --- | --- |
| `listOutboundMessages` | query | MessagePage |
| `getMessageDetails` | query | MessageDetails |

`sendEmail` is omitted because Postmark's public Messages API does not document a repeat-safe send deduplication key. Outbound messages expire after the server's retention period.

## Authorization

Start at [https://account.postmarkapp.com/servers](https://account.postmarkapp.com/servers) and follow
**Servers > select a server > API Tokens**. Create a server, open its API Tokens tab, and copy the Server API token. It is a secret opaque string; blank prevents requests. Paste the token in the
secret `api_key` field in Dex Web Connections.
The key travels only in `X-Postmark-Server-Token`. Leave `baseUrl`, `timeout`,
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
| HTTP 408, 429, 5xx, transport interruption | Retry until the default budget ends, then Flow failure |

Every non-happy branch is optional. The example wires only the happy branch;
an unwired optional outcome fails the Flow. Provider error response text and
credentials never enter a Dex result, receipt, Stream, or log.

## Mocks

`postmarkmock.New(t, "mock")` binds the default manifest answers and the
generated `zz_generated_mock_test.go` strictly decodes every fixture. The
checked-in [`workflow_mock_integration_test.go`](examples/outbound-lookup/flow/workflow_mock_integration_test.go)
uses the same generated mock against a real Dex Worker in CI. Its start looks
like this runnable source:

```go
mock := postmarkmock.New(t, ConnectionName)
flow := NewFlow(mock.Connection())
```

## Provider documentation and verification

- [https://postmarkapp.com/developer/api/messages-api](https://postmarkapp.com/developer/api/messages-api)
- [https://postmarkapp.com/developer/api/overview](https://postmarkapp.com/developer/api/overview)

The HTTP unit tests verify request shapes and local classification against
an `httptest` server. Provider authorization, live API response variations,
and real Dex integration remain unverified locally. CI runs the mock backed
real Dex test; a user-controlled live connection is still needed to confirm
the provider account and permissions.
