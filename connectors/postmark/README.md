# Postmark connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/postmark/go@v0.4.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/postmark`.

This Dex connector calls the public Postmark JSON API. Its
manifest version is `v0.4.0`; the Go module requires connector SDK `v0.38.0`
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
| `sendEmail` | limited mutation | SentEmail |
| `listTemplates` | query | TemplatePage |
| `getTemplate` | query | Template |
| `createTemplate` | limited mutation | Template |
| `updateTemplate` | safe mutation | Template |
| `deleteTemplate` | safe mutation | DeletedTemplate |
| `listBounces` | query | BouncePage |
| `getBounce` | query | Bounce |
| `activateBounce` | safe mutation | Bounce |
| `listSuppressions` | query | SuppressionPage |
| `createSuppression` | safe mutation | SuppressionAction |
| `deleteSuppression` | safe mutation | SuppressionAction |
| `sendEmailWithTemplate` | limited mutation | SentEmail |
| `listInboundMessages` | query | InboundMessagePage |
| `getInboundMessageDetails` | query | InboundMessage |
| `bypassInboundMessage` | limited mutation | InboundMessageAction |
| `retryInboundMessage` | limited mutation | InboundMessageAction |

`sendEmail` accepts one plain text message from a verified sender. Postmark does
not document a deduplication key for sends, so the connector uses Dex's durable
dispatch fence and a call-specific `dex_call_id` metadata marker. After an unknown
outcome it searches outbound activity for that marker before another possible send.
It permits up to three possible sends and selects `uncertain` if the cap is reached
without confirmation. Outbound messages expire after the server's retention period.
`sendEmailWithTemplate` sends through a server template alias with a rendering
model and applies the same three-send fence and unique outbound-activity marker.
Keep secrets out of template model values because they enter the Flow input.

`createTemplate` assigns a unique `dex-<call ID>` alias so a retry can read back
the exact new template before another possible creation. Supply the template name,
subject, and a text or HTML body. `updateTemplate` sends the complete replacement
content; `deleteTemplate` treats an already missing template as deleted.

`listBounces` and `getBounce` inspect delivery failures for the current server.
`activateBounce` applies Postmark's recipient reactivation action to a bounce ID;
use it only after deciding the recipient should receive mail again.

Suppression operations require a message stream ID from **Servers > select a
server > Message Streams** at `https://account.postmarkapp.com/servers` and an
email address. The list operation can filter by recipient. Create and delete
apply the requested recipient state; a per-address `Failed` result selects
`providerRejected`, even when Postmark returns HTTP 200. Postmark does not allow
deletion of a spam-complaint suppression.

Inbound search and lookup return message metadata without bodies or attachments.
`bypassInboundMessage` requests processing of a blocked email;
`retryInboundMessage` reschedules a failed one. Both use a durable dispatch
fence, check the same message's processing status after an unknown outcome, and
allow at most three possible sends before selecting `uncertain`.

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
| Query HTTP 408, 429, 5xx, transport interruption | Retry until the default budget ends, then Flow failure |
| `sendEmail` unknown outcome | Read back by `dex_call_id`; retry up to three possible sends, then `uncertain` |

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
- [https://postmarkapp.com/developer/api/email-api](https://postmarkapp.com/developer/api/email-api)
- [https://postmarkapp.com/developer/api/templates-api](https://postmarkapp.com/developer/api/templates-api)
- [https://postmarkapp.com/developer/api/bounce-api](https://postmarkapp.com/developer/api/bounce-api)
- [https://postmarkapp.com/developer/api/suppressions-api](https://postmarkapp.com/developer/api/suppressions-api)
- [https://postmarkapp.com/developer/api/overview](https://postmarkapp.com/developer/api/overview)

The HTTP unit tests verify request shapes and local classification against
an `httptest` server. The TypeScript example's Worker and Client integration
test exercises a local provider stand-in through real Dex. Provider
authorization and live API response variations remain unverified; a
user-controlled live connection is still needed to confirm the account and
permissions.
