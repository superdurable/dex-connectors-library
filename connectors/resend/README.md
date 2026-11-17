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
| `createTopic` | mutation | TopicReference |
| `getTopic` | query | Topic |
| `listTopics` | query | TopicPage |
| `updateTopic` | mutation | TopicReference |
| `deleteTopic` | mutation | DeletedTopic |
| `listEmailAttachments` | query | EmailAttachmentPage |
| `getEmailAttachment` | query | EmailAttachment |
| `listReceivedEmails` | query | ReceivedEmailPage |
| `getReceivedEmail` | query | ReceivedEmail |
| `listReceivedEmailAttachments` | query | EmailAttachmentPage |
| `getReceivedEmailAttachment` | query | EmailAttachment |
| `getEmailMetrics` | query | EmailMetrics |
| `createSegment` | mutation | SegmentReference |
| `getSegment` | query | Segment |
| `listSegments` | query | SegmentPage |
| `updateSegment` | mutation | SegmentReference |
| `deleteSegment` | mutation | DeletedSegment |
| `listSegmentContacts` | query | ContactPage |
| `addContactToSegment` | mutation | ContactSegmentReference |
| `removeContactFromSegment` | mutation | RemovedContactSegment |
| `listContactSegments` | query | SegmentPage |
| `getContactTopics` | query | ContactTopicPage |
| `updateContactTopics` | mutation | UpdatedContactTopics |
| `listContactImports` | query | ContactImportPage |
| `getContactImport` | query | ContactImport |
| `createContactProperty` | mutation | ContactPropertyReference |
| `getContactProperty` | query | ContactProperty |
| `listContactProperties` | query | ContactPropertyPage |
| `updateContactProperty` | mutation | ContactPropertyReference |
| `deleteContactProperty` | mutation | DeletedContactProperty |
| `createBroadcast` | mutation | BroadcastReference |
| `getBroadcast` | query | Broadcast |
| `listBroadcasts` | query | BroadcastPage |
| `updateBroadcast` | mutation | BroadcastReference |
| `deleteBroadcast` | mutation | DeletedBroadcast |
| `sendBroadcast` | mutation | BroadcastReference |
| `cancelBroadcast` | mutation | BroadcastReference |
| `duplicateBroadcast` | mutation | BroadcastReference |
| `listBroadcastRecipients` | query | BroadcastRecipientPage |
| `listBroadcastClickedLinks` | query | BroadcastClickedLinkPage |

Topic CRUD follows [Resend's Topics API](https://resend.com/docs/api-reference/topics/create-topic).
`createTopic` is `limited` with three possible sends and read-back by topic
name plus settings; an unconfirmed cap selects optional `uncertain`.
`getTopic` and `listTopics` are safe queries. `updateTopic` sets only the
selected absolute fields, and `deleteTopic` is repeat-safe. Find topic IDs
under [Resend Topics](https://resend.com/topics); blank IDs are invalid.
`defaultSubscription` is immutable and must be `opt_in` or `opt_out`.
Names are at most 50 characters, descriptions at most 200, and visibility is
`public` or `private`; blank visibility on creation uses private. See the
checked-in [create-topic Flow](go/examples/create-topic/README.md) for a
mock-backed Dex Worker and Client run.

Segment CRUD follows [Resend's Segments API](https://resend.com/docs/api-reference/segments/create-segment).
`createSegment` uses the `limited` tier, checks names across paginated
read-back, and caps possible sends at three. `getSegment`, `listSegments`,
and `listSegmentContacts` are safe reads; `updateSegment` is an absolute
rename, and `deleteSegment` is repeat-safe. Find segment IDs under
[Resend Segments](https://resend.com/segments); blank names and IDs are
invalid. The [create-segment Flow](go/examples/create-segment/README.md)
demonstrates the generated factory.

Contact segment membership and topic subscription operations use the `safe`
repeat tier: a repeated add, remove, or absolute subscription assignment
converges on the requested state. Contact IDs or email addresses come from
[Resend Contacts](https://resend.com/contacts); segment and topic IDs come
from their respective dashboard pages. Blank IDs are invalid. Topic choices
are `opt_in` or `opt_out`; pass one entry per topic whose subscription
should change. See the
[add-contact-to-segment Flow](go/examples/add-contact-to-segment/README.md)
for the generated factory and a real Dex mock run.

`listContactImports` and `getContactImport` are safe queries for import
progress and row counts. The list accepts a 1–100 `limit`, exclusive `after`
cursor, and optional `queued`, `in_progress`, `completed`, or `failed`
status. Find import IDs under [Resend Contacts](https://resend.com/contacts).
Creating an import requires multipart CSV upload and is outside this JSON
connector's supported operation shape.

Contact property CRUD follows [Resend's Contact Properties API](https://resend.com/docs/api-reference/contact-properties/create-contact-property).
`createContactProperty` is `limited` with three possible sends and read-back
by the unique key, type, and fallback value. Reads, absolute fallback
replacement, and deletion are `safe`. Property keys allow only ASCII letters,
digits, and underscores, up to 50 characters; type is `string` or `number`.
An update supplies a new fallback value of the property's type. Find property
IDs under [Resend Contacts](https://resend.com/contacts). The
[create-contact-property Flow](go/examples/create-contact-property/README.md)
exercises the generated factory through real Dex with a provider mock.

Broadcast creation always makes a draft; delivery is a separate
`sendBroadcast` step. `createBroadcast` uses `limited` with three possible
sends and paginated read-back by draft name, segment, sender, subject, topic,
and message body. `duplicateBroadcast` also uses `limited`: Resend does not
expose a correlation field for copies, so an unknown copy cannot be confirmed
by read-back, and the cap selects optional `uncertain`. Reads, absolute draft
updates, draft deletion, send by existing ID, and cancellation use `safe`.
Scheduled send input is RFC 3339; blank sends immediately. A canceled scheduled
broadcast returns to draft, while a canceled queued broadcast stops remaining
recipients. Recipient pages require an event `type`; `bounceType` applies
only to bounced events. Find IDs under
[Resend Broadcasts](https://resend.com/broadcasts) and see the
[create-broadcast Flow](go/examples/create-broadcast/README.md) for the
generated factory. Broadcast read results omit message bodies from durable
Flow state.

Sent and received attachment queries return metadata but omit Resend's
short-lived signed `download_url`, because that URL grants access to content.
Received-email queries return identifying metadata and attachment summaries;
message bodies and headers are omitted from durable Flow Results. Attachment
pages accept a 1–100 `limit` and an exclusive `after` cursor. Find email
IDs under [Resend Emails](https://resend.com/emails); blank IDs are invalid.
`getEmailMetrics` accepts ISO 8601 dates, an IANA timezone, hourly/daily/
weekly/monthly granularity, named measures and dimensions, and up to 100 IDs
per domain/email/broadcast filter. Email and broadcast filters are mutually
exclusive. Blank filters use the provider's defaults. The checked-in
[email-metrics Flow](go/examples/email-metrics/README.md) exercises the
generated query factory through real Dex with a provider mock.

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

## Inboxes

`CreateInbox` uses bounded `limited` retries and reads back by inbox email
after an unknown send. Get/list, absolute name updates, and deletion are
safe. Forwarding provisions a receiving address when the team has an inbound
domain. The runnable [`create-inbox` example](go/examples/create-inbox/README.md)
exercises creation through a real Dex Worker and generated provider mock.

## Automations

`CreateAutomation` and `DuplicateAutomation` use bounded `limited` retries.
Create reads back matching name, status, and graph after an unknown send.
Get/list, absolute updates, deletion, stop, and run status reads are safe.
Results omit automation step configurations, run outputs, and errors because
they can contain message content. The runnable
[`create-automation` example](go/examples/create-automation/README.md)
exercises graph creation through a real Dex Worker and generated provider mock.

## Automation events

`CreateEvent` and `SendEvent` use bounded `limited` retries. Create reads
back the name and schema after an unknown send; a delivery may be repeated
at most three times. Get/list, absolute schema updates, and deletion are
safe. `UpdateEvent` can clear the schema with `clearSchema`. Event payloads
are accepted as inputs but never returned by the connector. The runnable
[`create-event` example](go/examples/create-event/README.md) exercises the
create path through a real Dex Worker and generated provider mock.

## Webhooks

`CreateWebhook` and `ReplayWebhookEvent` use bounded `limited` retries;
create reads back matching endpoint and event subscriptions. Get/list,
absolute updates, deletion, and delivery history reads are safe. Webhook
results omit signing secrets, event payloads, and receiver response bodies.
Secret rotation is not exposed because the only useful response is a new
signing secret, which must not enter Dex Results. The runnable
[`create-webhook` example](go/examples/create-webhook/README.md) exercises
creation through a real Dex Worker and generated provider mock.

## Suppressions

`CreateSuppression` and `BatchAddSuppressions` use bounded `limited` retries
and read back addresses after an unknown send. Get/list, single delete, and
batch removal are safe operations. Batch inputs support up to 100 addresses
or IDs. The runnable [`create-suppression` example](go/examples/create-suppression/README.md)
exercises the create path through a real Dex Worker and generated provider mock.

## Templates

`CreateTemplate` and `DuplicateTemplate` use bounded `limited` retries;
create reads back matching template metadata and HTML after an unknown send.
Get/list, absolute updates, delete, and publish are safe operations. Template
read results omit message bodies and variable fallbacks. The runnable
[`create-template` example](go/examples/create-template/README.md) exercises
the create path through a real Dex Worker and generated provider mock.

## Account observability

`GetUsage`, `ListLogs`, and `GetLog` are safe reads. Usage exposes daily and
monthly counters; the optional `period` input can select one period. Log
results deliberately omit provider request and response bodies because those
can contain credentials or message content. The runnable
[`account-usage` example](go/examples/account-usage/README.md) verifies the
query through a real Dex Worker and generated provider mock.

## Inbox agents and labels

`GetInboxAgent` exposes tone and enabled actions but deliberately omits
instructions, which may contain sensitive content. `UpdateInboxAgent` makes
absolute changes and supports explicit clears. Label listing and retrieval
are safe reads, as are absolute label updates and deletion. `CreateInboxLabel`
uses bounded `limited` retries and reads back the unique case-insensitive
label name before another send. The runnable
[`create-inbox-label` example](go/examples/create-inbox-label/README.md)
exercises label creation through a real Dex Worker and generated provider mock.

## Inbox threads and messages

`ListInboxThreads`, `SearchInboxThreads`, and `GetInboxThread` expose
conversation metadata. `UpdateInboxThread` changes read state or folder;
`DeleteInboxThread` moves the thread to trash. Add, set, and remove label
operations are safe to repeat. Thread email reads omit message bodies,
attachments, and headers from Dex Results. `ReplyInboxThreadEmail` and
`ForwardInboxThreadEmail` use the provider's 24-hour idempotency key, so
unknown outcomes can repeat safely. The runnable
[`update-inbox-thread` example](go/examples/update-inbox-thread/README.md)
verifies one write through a real Dex Worker and generated provider mock.

## Inbox drafts

`ListInboxDrafts` and `GetInboxDraft` expose metadata without message bodies
or attachments. `CreateInboxDraft` and `SendInboxDraft` use Resend's
24-hour idempotency key and safe retry tier. Absolute `UpdateInboxDraft`
fields and `DeleteInboxDraft` are also safe to repeat. Attachment upload is
outside this JSON example because it requires binary content or remote URL
retrieval. The runnable
[`create-inbox-draft` example](go/examples/create-inbox-draft/README.md)
verifies draft creation through a real Dex Worker and generated provider mock.

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
