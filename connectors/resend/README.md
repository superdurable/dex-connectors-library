# Resend connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/resend/go@v0.2.1`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/resend`.

This Dex connector calls the public Resend JSON API. Its
manifest version is `v0.2.1`; the Go module requires connector SDK `v0.33.0`
    and Dex Go SDK `v1.26.0` or newer. The runnable
[`welcome-email` example](go/examples/welcome-email/README.md) shows a
complete Worker and the generated operation-specific Step factory.

The Python package is [`dex-connector-resend`](python/README.md), imported as
`dex_connectors.resend`.

The TypeScript package is [`@superdurable/connector-resend`](typescript/README.md).
Its [welcome email example](typescript/examples/welcome-email/README.md) registers
the generated `newSendEmailStep` with the static `resend` connection name and
verifies the Flow through a real Dex Worker and Client. Provider HTTP behavior
is replayed against Go-recorded scenarios; live Resend authorization remains
unverified.

## Operations

| Operation | Kind | Output |
| --- | --- | --- |
| `sendEmail` | mutation | SentEmail |
| `getEmail` | query | Email |

`sendEmail` sends the stable Dex call idempotency key in `Idempotency-Key`. Resend documents 24-hour key retention; the connector's default one-hour retry budget stays within that window. Keep any per-Step retry override shorter than 24 hours. The manifest therefore declares `sendEmail` `repeatOnUnknown: safe`. Every 409 retries with the same key, including `invalid_idempotent_request`, because the conflict cannot prove that no email was sent; only HTTP 400, 401, 403, 404, 411, 413, 414, 415, or 422 selects `providerRejected`, and any other status or a malformed or oversized answer retries.

## Authorization

Before sending, verify the sender domain in Resend under **Domains**; supply an address on that domain in the Flow input.

Start at [https://resend.com/api-keys](https://resend.com/api-keys) and follow
**API Keys > Create API Key**. Create an API key with sending permission, then copy its secret value. It is an opaque secret string; blank prevents requests. Paste the token in the
secret `api_key` field in Dex Web Connections.
The key travels only in `Authorization`. Leave `baseUrl`, `timeout`,
and `maxResponseBytes` blank for the displayed public endpoint and safety
defaults; override the base URL only for a compatible gateway or test server.

## Failures and retries

| Condition | Result |
| --- | --- |
| Successful documented response | operation's happy branch |
| Known missing resource on a detail operation | `notFound` |
| Other definite 4xx rejection; for `sendEmail` only HTTP 400, 401, 403, 404, 411, 413, 414, 415, or 422 | `providerRejected` |
| Malformed or oversized successful JSON from `getEmail` | `invalidResponse` |
| Any other `sendEmail` status, including every 409, or a malformed or oversized `sendEmail` answer | Retry with the same Idempotency-Key |
| Invalid local input or credentials, malformed stored credential material, or a connection that needs reauthorization, is missing or no longer selected, or that the credential broker refused | `defect` |
| HTTP 408, 429, 5xx, transport interruption, or any other credential lookup failure, such as an unavailable broker or a connection that is not configured yet | Retry until the default budget ends, then Flow failure |

Every non-happy branch is optional. The example wires only the happy branch;
an unwired optional outcome fails the Flow. Provider error response text and
credentials never enter a Dex result, receipt, Stream, or log.

## Mocks

`resendmock.New(t, "mock")` binds the default manifest answers and the
generated `zz_generated_mock_test.go` strictly decodes every fixture. The
checked-in [`workflow_mock_integration_test.go`](go/examples/welcome-email/flow/workflow_mock_integration_test.go)
uses the same generated mock against a real Dex Worker in CI. Its start looks
like this runnable source:

```go
mock := resendmock.New(t, ConnectionName)
flow := NewFlow(mock.Connection())
```

## Provider documentation and verification

- [https://resend.com/docs/api-reference/emails/send-email](https://resend.com/docs/api-reference/emails/send-email)
- [https://resend.com/docs/api-reference/emails/retrieve-email](https://resend.com/docs/api-reference/emails/retrieve-email)
- [https://resend.com/changelog/idempotency-keys](https://resend.com/changelog/idempotency-keys)

The HTTP unit tests verify request shapes and local classification against
an `httptest` server. Provider authorization, live API response variations,
and real Dex integration remain unverified locally. CI runs the mock backed
real Dex test; a user-controlled live connection is still needed to confirm
the provider account and permissions.
