# Postmark connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/postmark/go@v0.3.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/postmark`.

This Dex connector calls the public Postmark JSON API. Its
manifest version is `v0.3.0`; the Go module requires connector SDK `v0.33.0`
    and Dex Go SDK `v1.5.0` or newer. The runnable
[`outbound-lookup` example](go/examples/outbound-lookup/README.md) shows a
complete Worker and the generated operation-specific Step factory.

The Python package is [`dex-connector-postmark`](python/README.md), imported as
`dex_connectors.postmark`.

The TypeScript package is [`@superdurable/connector-postmark`](typescript/README.md).
Its [outbound lookup example](typescript/examples/outbound-lookup/README.md)
uses the static `postmark` connection and a real Dex Worker and Client.

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
checked-in [`workflow_mock_integration_test.go`](go/examples/outbound-lookup/flow/workflow_mock_integration_test.go)
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
an `httptest` server. The TypeScript example's Worker and Client integration
test exercises a local provider stand-in through real Dex. Provider
authorization and live API response variations remain unverified; a
user-controlled live connection is still needed to confirm the account and
permissions.
