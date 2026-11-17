# Resend connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/resend/go@v0.3.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/resend`.

This Dex connector calls the public Resend JSON API. Its
manifest version is `v0.3.0`; the Go module requires connector SDK `v0.38.0`
    and Dex Go SDK `v1.26.0` or newer. The runnable
[`welcome-email` example](go/examples/welcome-email/README.md) shows a
complete Worker and the generated operation-specific Step factory.
The [`manage-contact` example](go/examples/manage-contact/README.md) exercises
an absolute contact update through a real Dex Worker in its mock test.

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
| `listEmails` | query | EmailPage |
| `getContact` | query | Contact |
| `listContacts` | query | ContactPage |
| `getDomain` | query | Domain |
| `listDomains` | query | DomainPage |
| `updateContact` | mutation | ContactReference |
| `deleteContact` | mutation | DeletedContact |
| `deleteDomain` | mutation | DeletedDomain |
| `updateScheduledEmail` | mutation | EmailReference |
| `cancelScheduledEmail` | mutation | EmailReference |
| `createContact` | mutation | ContactReference |
| `createDomain` | mutation | DomainReference |
| `updateDomain` | mutation | DomainReference |
| `verifyDomain` | mutation | DomainReference |
| `sendBatchEmails` | mutation | SentEmailPage |

The five new Go queries use the `safe` repeat tier and bounded HTTP responses.
List inputs accept a 1–100 item `limit`; zero uses Resend's default for emails
and contacts, or retrieves all domains. `after` is an exclusive ID cursor;
domain pages require `limit` when using it. These operations are Go-only in
this release; Python and TypeScript retain their declared email capabilities.

The five new Go writes use `safe` repeat semantics: contact fields and delivery
time are absolute replacements; deletion and cancellation converge on one
terminal state. Resend's contact, domain, and scheduled-email API pages define
their IDs and request formats. Supply a contact ID or email from **Contacts**,
a domain ID from **Domains**, or a scheduled email ID from **Emails**; blank IDs
are invalid. `scheduledAt` is an RFC 3339 timestamp with a timezone. These
fields are Flow inputs and never appear in Dex Web connection setup.

`createContact` and `createDomain` use the `limited` repeat tier and a maximum
of three possibly effective sends. Resend documents an idempotency key for
email sending and batch sending, but not these create endpoints. The connector
reads back by unique email or domain name after an unknown outcome, then
resends only while the Dex Fence permits. A reached cap with no confirmation
selects optional `uncertain`. Enter a contact email from **Contacts** or a DNS
domain name in **Domains**; blank or invalid identities are rejected locally.

`updateDomain` sets selected absolute tracking, TLS, and sending/receiving
capability fields with the `safe` tier. Omitted pointers preserve current
settings, while `false` disables tracking. `tls` is `opportunistic` or
`enforced`; capabilities are `enabled` or `disabled`, and at least one must
remain enabled. `verifyDomain` starts Resend's asynchronous DNS verification
cycle; repeating it is harmless and uses the `safe` tier. Its successful
branch confirms only that verification started, not that DNS is verified.
Find the domain ID under [Resend Domains](https://resend.com/domains), then
consult [update](https://resend.com/docs/api-reference/domains/update-domain)
and [verify](https://resend.com/docs/api-reference/domains/verify-domain)
for settings and DNS setup. The [`verify-domain` Flow](go/examples/verify-domain/README.md)
exercises the generated factory through real Dex with a provider mock. Domain
claim is dashboard-only; Resend has not published a claim API.

`sendEmail` sends the stable Dex call idempotency key in `Idempotency-Key`. Resend documents 24-hour key retention; the connector's default one-hour retry budget stays within that window. Keep any per-Step retry override shorter than 24 hours. A 409 `concurrent_idempotent_requests` retries with the same key, while `invalid_idempotent_request` selects `providerRejected`.

`sendBatchEmails` sends 1–100 complete email messages in one POST with the same
provider idempotency protection as `sendEmail`. Each message needs a verified
`from` address, 1–50 `to` recipients, subject, and HTML. The result contains
the provider ID of every queued email in input order. The operation uses the
`safe` tier because Resend deduplicates by `Idempotency-Key` for 24 hours; keep
retry overrides shorter than that retention period. See
[Resend batch sending](https://resend.com/docs/api-reference/emails/send-batch-emails)
and the runnable [`send-batch-emails` Flow](go/examples/send-batch-emails/README.md).

## Authorization

Before sending, verify the sender domain in Resend under **Domains**; supply an address on that domain in the Flow input.

Start at [https://resend.com/api-keys](https://resend.com/api-keys) and follow
**API Keys > Create API Key**. Create a full-access API key to use contact and
domain reads and writes; a sending-only key still supports `sendEmail`. Copy its opaque
secret value; blank prevents requests. Paste the token in the
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
| HTTP 408, 429, 5xx, transport interruption | Retry until the default budget ends, then Flow failure |

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
- [https://resend.com/docs/api-reference/emails/list-emails](https://resend.com/docs/api-reference/emails/list-emails)
- [https://resend.com/docs/api-reference/contacts/list-contacts](https://resend.com/docs/api-reference/contacts/list-contacts)
- [https://resend.com/docs/api-reference/domains/list-domains](https://resend.com/docs/api-reference/domains/list-domains)
- [https://resend.com/docs/api-reference/contacts/update-contact](https://resend.com/docs/api-reference/contacts/update-contact)
- [https://resend.com/docs/api-reference/contacts/delete-contact](https://resend.com/docs/api-reference/contacts/delete-contact)
- [https://resend.com/docs/api-reference/domains/delete-domain](https://resend.com/docs/api-reference/domains/delete-domain)
- [https://resend.com/docs/api-reference/emails/update-email](https://resend.com/docs/api-reference/emails/update-email)
- [https://resend.com/docs/api-reference/emails/cancel-email](https://resend.com/docs/api-reference/emails/cancel-email)
- [https://resend.com/docs/api-reference/contacts/create-contact](https://resend.com/docs/api-reference/contacts/create-contact)
- [https://resend.com/docs/api-reference/domains/create-domain](https://resend.com/docs/api-reference/domains/create-domain)
- [https://resend.com/changelog/idempotency-keys](https://resend.com/changelog/idempotency-keys)

The HTTP unit tests verify request shapes and local classification against
an `httptest` server. Provider authorization, live API response variations,
and real Dex integration remain unverified locally. CI runs the mock backed
real Dex test; a user-controlled live connection is still needed to confirm
the provider account and permissions.
