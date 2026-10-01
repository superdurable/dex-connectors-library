# Xero Connector

The Xero Connector reads and writes the Xero Accounting API
(`https://api.xero.com/api.xro/2.0`) from Dex Flows. It exposes these
operation-specific Dex Step factories:

| Operation | Kind | Happy branch | Other branches |
| --- | --- | --- | --- |
| `xero.NewListContactsStep` | Query | `listed` | `providerRejected`, `dailyLimitReached`, `invalidResponse`, `defect` |
| `xero.NewFindContactByEmailStep` | Query | `found` | `notFound`, `ambiguous`, `providerRejected`, `dailyLimitReached`, `invalidResponse`, `defect` |
| `xero.NewGetInvoiceStep` | Query | `found` | `notFound`, `providerRejected`, `dailyLimitReached`, `invalidResponse`, `defect` |
| `xero.NewListInvoicesStep` | Query | `listed` | `providerRejected`, `dailyLimitReached`, `invalidResponse`, `defect` |
| `xero.NewCreateInvoiceStep` | Mutation | `created` | `providerRejected`, `dailyLimitReached`, `uncertain`, `defect` |
| `xero.NewRecordPaymentStep` | Mutation | `recorded` | `providerRejected`, `dailyLimitReached`, `uncertain`, `defect` |

Only the happy-path branch of each operation is required; every other branch
is optional, and an unwired optional branch fails the Flow. Every operation
uses async Execute durability and a 30-second Execute timeout. Reads retry
for up to five minutes; the two writes retry for up to four minutes, which
keeps every attempt inside Xero's six-minute Idempotency-Key lifetime.

This release has no Triggers and no Studio pickers; see
[Not in this release](#not-in-this-release).

## Xero setup

A connection uses one of two methods. Dex Web shows the Custom Connection
first.

### Custom Connection (recommended)

A [Custom Connection](https://developer.xero.com/documentation/guides/oauth2/custom-connections)
is a Xero app that reaches exactly one organisation and authenticates as
itself with Xero's client credentials grant. It suits a back-office Flow that
always books into the same organisation. Xero offers it only to organisations
in Australia, New Zealand, the UK, and the US, and the organisation needs a
Custom Connection subscription; the Xero Demo Company is free for development.

1. At `https://developer.xero.com/app/manage` choose **New app** and select
   **Custom connection**.
2. Select the scopes `accounting.invoices`, `accounting.payments`, and
   `accounting.contacts.read`, and name the Xero user who authorises it. That
   user receives an email, chooses **Connect**, and selects the organisation.
3. Once Xero reports the connection authorised, open the app's
   **Configuration** page, copy the Client ID, generate a client secret, and
   enter both in Dex Web.

The client ID is a 32-character hexadecimal string; the secret is shown once.
The connector exchanges them at `https://identity.xero.com/connect/token`
with `grant_type=client_credentials` and exactly those three scopes, sending
the client as HTTP Basic credentials as Xero documents. Xero returns a
30-minute access token, which the connector requests again five minutes
before it expires. Locally, `localconfig` writes the token and its expiry
back to the connection file atomically, so one token serves every Step until
it nears expiry. A Custom Connection sends no `Xero-Tenant-Id` header,
because Xero's Custom Connection guide calls the API with the token alone.

This method works in Dex Web today: the form saves a client ID and secret,
and the Worker obtains every token itself.

### Xero OAuth 2.0 web app

For a Xero web app that a user authorises through OAuth consent:

1. At `https://developer.xero.com/app/manage` choose **New app** and select
   **Web app**.
2. Open Dex Web at `http://localhost:<port>` rather than `127.0.0.1`: Xero
   accepts `http://localhost` redirect URIs for testing and rejects
   `http://127.0.0.1`. Add the Redirect URI that Dex Web shows to the app's
   **Configuration > Redirect URIs**.
3. Copy the Client ID and a generated client secret into Dex Web and choose
   **Authorize**. Dex requests `offline_access accounting.invoices
   accounting.payments accounting.contacts.read`.

The connector refreshes the 30-minute access token with Xero's rotating
refresh token, presenting the client as HTTP Basic credentials, and stores
the replacement refresh token Xero returns every time. A refresh token unused
for 60 days expires, and the connection must be authorized again.

A user can connect several organisations in one consent. Every OAuth request
therefore sends `Xero-Tenant-Id`, chosen by the method's `organisation`
configuration field:

- a tenant ID (a UUID) is sent as is;
- an organisation name is matched, ignoring case, against the `ORGANISATION`
  tenants that `GET https://api.xero.com/connections` returns;
- blank uses the only connected organisation, and fails when there are none
  or several.

The connector remembers a tenant it read from `/connections` and reads the
list again after Xero answers 403 for it. A selection failure selects
`defect` without naming the connected organisations.

#### Dex Web authorization caveats

Dex Web exchanges the authorization code with a form body that carries
`client_id` and `client_secret`. Xero's guide shows HTTP Basic for the code
exchange, and the manifest schema cannot yet declare Dex Web `cli-v1.4.0`'s
`tokenEndpointAuthMethod`, but Xero's OpenID discovery document at
`https://identity.xero.com/.well-known/openid-configuration` lists both
`client_secret_basic` and `client_secret_post`, so the form body is
accepted. Xero's guide does not list `scope` among the code exchange's
response fields, although it lists it for the client credentials response.
Dex Web `cli-v1.4.0`, the release in `.dex-compat-version`, accepts a token
response without `scope` and checks the scopes only when Xero returns them;
`cli-v1.2.0` and earlier reject such a grant with
`CONNECTOR_OAUTH_SCOPE_INSUFFICIENT`, and the Custom Connection is then the
working method. The exchange was not verified against a live Xero app.

PKCE apps (Xero's "Mobile or desktop app" type) have no client secret and are
not supported by this release.

## Local configuration

Dex Web writes one record per connection. For a Custom Connection, the
credentials hold `auth_method: custom-connection`, `client_id`, and
`client_secret`; the connector adds `access_token`, and `localconfig` adds
`credentialExpiresAt`. For OAuth they hold `auth_method: xero-oauth`, the
client ID and secret, and the `access_token` and `refresh_token` from
consent, and the configuration may hold `organisation`.

Load it with `localconfig.LoadFromEnvironment` and `xero.NewLocalConnection`,
as [`examples/approved-order/main.go`](examples/approved-order/main.go) does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := xero.NewLocalConnection(store, approvedorder.ConnectionName, connectionOptions()...)
```

Credentials are reread before every provider call, so a replaced secret takes
effect without a restart. The organisation and response limit are startup
configuration.

## Hosted credentials

In Superverse-hosted deployments, construct the client with the
operation-scoped broker provider. `DecodeResolvedCredentialsJSON` accepts
exactly `auth_method` and `access_token` and rejects client secrets, refresh
tokens, and anything else without repeating a value.

## Amounts, currencies, and Xero values

Every amount is an `xero.Decimal`, an exact base-10 string such as
`"1800.00"`. The connector never converts one to floating point:

- an input is validated as a plain literal, without an exponent, a leading
  zero, or a thousands separator, and sent to Xero as the same JSON number;
- a value Xero returns, as a JSON number or a numeric string, keeps Xero's
  exact digits, including trailing zeros;
- `Decimal.IsZero` and `Decimal.IsNegative` compare without parsing.

Quantities and unit amounts accept four decimal places, and every invoice
request sends `unitdp=4`, so Xero neither rounds a four-place unit price nor
returns a rounded one. Payment amounts accept two. Every amount is in the
invoice's currency: `currencyCode` is an ISO 4217 code enabled in the
organisation, and blank uses the organisation's base currency.

Types, statuses, and line amount types are Xero's own values, never mapped to
another vocabulary: `ACCREC` and `ACCPAY`; `DRAFT`, `SUBMITTED`,
`AUTHORISED`, `PAID`, `VOIDED`, and `DELETED`; `Exclusive`, `Inclusive`,
and `NoTax`; contact statuses `ACTIVE`, `ARCHIVED`, and `GDPRREQUEST`.
Results pass through any value Xero adds later; inputs are validated before
any request. Xero's .NET dates are returned as `YYYY-MM-DD`, from
`DateString` when present, and timestamps as UTC instants.

## Operations

### listContacts and findContactByEmail

`listContacts` reads one page of `GET /Contacts` with an optional
case-insensitive `searchTerm` (Name, FirstName, LastName, ContactNumber,
CompanyNumber, and EmailAddress), up to 40 `contactIds`,
`includesArchived`, and `modifiedSince`, sent as `If-Modified-Since` in
Xero's `yyyy-mm-ddThh:mm:ss` UTC format. `pageSize` is 1 to 100, 25 by
default, and the page reports Xero's `pageCount` and `itemCount`.

`findContactByEmail` resolves one contact through Xero's optimised filter
`where=EmailAddress=="jane@example.com"`. Xero's filter ignores case and
accents, so the connector keeps only contacts whose address equals the input
ignoring case, and only `ACTIVE` ones unless `includesArchived` is set. It
selects `found` for one match, `notFound` for none, and `ambiguous` with up
to ten `matches` for several. An address that is not one bare address, or
that holds a quote or backslash that could change the filter, selects
`defect` without a request.

### getInvoice and listInvoices

`getInvoice` reads `GET /Invoices/{InvoiceID}` or
`GET /Invoices/{InvoiceNumber}` with line items, totals, `AmountDue`,
`AmountPaid`, and applied payments. An answer about another invoice selects
`invalidResponse`.

`listInvoices` reads one page of `GET /Invoices` with line items, using
Xero's optimised filters: `Statuses`, `ContactIDs`, and `InvoiceNumbers`
(up to 40 each), and a `where` filter for `type` and an exact `reference`,
such as `Type=="ACCREC" AND Reference=="ORDER-1042"`
(`xero.BuildInvoiceWhereFilter` returns it for review). `modifiedSince` is
sent as `If-Modified-Since`; Xero notes that some changes, such as a due date
on a partly paid invoice, do not update `UpdatedDateUTC`. Results are in
Xero's default order, last modification then ID.

### createInvoice

`createInvoice` sends `PUT /Invoices?unitdp=4`, which only creates, with the
type, the contact's `ContactID` only (Xero updates a contact when sent more),
1 to 100 lines with exact amounts, dates, line amount types, currency,
reference, branding theme, an optional invoice number, and a status of
`DRAFT`, `SUBMITTED`, or `AUTHORISED`. Only an `AUTHORISED` invoice can take
a payment. Blank `invoiceNumber` lets Xero number a sales invoice from the
organisation's invoice settings; an `ACCREC` number must be unique, and a
duplicate is `providerRejected`.

### recordPayment

`recordPayment` sends `PUT /Payments` against one approved invoice from a
`BANK` account, or an account with payments enabled, given by `accountId` or
`accountCode`, with the date, the exact amount, an optional `bankAmount` for
a multicurrency payment, a reference, and `isReconciled`. The amount must not
exceed the amount due. The result carries the payment and the invoice's
remaining `amountDue` as Xero reports it.

### Duplicate safety

Xero documents an
[`Idempotency-Key`](https://developer.xero.com/documentation/guides/idempotent-requests/idempotency)
header for `POST`, `PUT`, and `PATCH`: a repeated key with the same request
returns the first response instead of running again, a concurrent repeat
waits for the first and receives its response, a repeated key with a
different request returns 400, and a key expires six minutes after its first
use. Both writes send the Step's stable Call ID as the key, and their bodies
are deterministic for one input, so every attempt of one Step execution,
including Dex's async backup dispatch and a replay on a new Worker, sends the
same key and body:

- a lost response, a timeout, a 429 other than the daily limit, or a 502,
  503, or 504 is retried with the identical request, and Xero replays the
  first invoice or payment;
- a 500 selects `uncertain` without a retry: Xero caches an internal error
  under the key, so retrying cannot change the outcome, and the write may have
  partly run. Read back with `listInvoices` by reference, or `getInvoice`,
  before writing again with a new Step execution;
- an accepted 2xx that cannot be decoded also selects `uncertain`;
- the four-minute retry window plus the 30-second Execute timeout stays
  inside the six-minute key lifetime, because a repeat after expiry would be
  processed as new.

A new Step execution is a new logical write. Set `reference` to an
application ID, such as an order ID, and look it up with `listInvoices`
before creating, as the example does.

## Rate limits and errors

Xero limits each organisation to 5 concurrent calls, 60 calls a minute, and
1,000 (starter tier) or 5,000 calls a day, and each app to 10,000 calls a
minute. A 429 names the limit in `X-Rate-Limit-Problem`:

- the minute, app-minute, and concurrency limits are retried after
  `Retry-After`;
- the daily limit, `X-Rate-Limit-Problem: day` or any `Retry-After` longer
  than two minutes, selects `dailyLimitReached` with a `QUOTA_EXHAUSTED`
  Failure, and the Receipt metadata `retryAfterSeconds` holds Xero's full
  delay, so a Flow can wait with a durable Timer instead of failing a Step's
  retry window. Xero checks rate limits before idempotency, so nothing was
  written.

Every Receipt also carries `dayLimitRemaining`, `minuteLimitRemaining`, and
`appMinuteLimitRemaining` when Xero sends them, `tenantId` for an OAuth call,
and Xero's `Xero-Correlation-Id` as the provider request ID.

A Failure never repeats Xero's message text. It repeats only the exception
type, error number, and problem detail tokens, and the count of validation
errors, such as
`Xero rejected the request (HTTP 400) [ValidationException; error 10; 2 validation errors]`.

| Response | Result |
| --- | --- |
| 400 | `providerRejected`, `VALIDATION` |
| 401 | one token refresh and resend, then `providerRejected`, `AUTHENTICATION` |
| 403 | `providerRejected`, `AUTHORIZATION`; an OAuth tenant is read again next time |
| 404 | `notFound` where declared, otherwise `providerRejected` |
| 3xx | `providerRejected`, `PROTOCOL`; redirects are never followed |
| 429 daily | `dailyLimitReached`, `QUOTA_EXHAUSTED` |
| 429 other | Retry after `Retry-After` |
| 500 | Retry for a read; `uncertain` for a write |
| 408, 502, 503, 504, transport failure | Retry |
| 501, other 4xx | `providerRejected` |
| oversized, malformed, or credential-reflecting 2xx | `invalidResponse`, or `uncertain` for a write |
| rejected client, grant, or scope at the token endpoint | `providerRejected`, `AUTHENTICATION` |
| invalid input or connection configuration | `defect`, with no request |

## Not in this release

### Triggers

Xero webhooks require an
["intent to receive"](https://developer.xero.com/documentation/guides/webhooks/overview)
validation before Xero delivers events: the endpoint must answer every
correctly signed payload with 2xx and every incorrectly signed one with
`401 Unauthorized` within five seconds. `sdkgo/webhooktrigger` answers a
verification failure with 400 and has no handshake hook yet, so no Trigger
ships. Until then, poll with `listInvoices` or `listContacts` and
`modifiedSince`.

### Studio pickers

An organisation or account picker would call `GET
https://api.xero.com/connections` or `GET /Accounts`, but Studio setup
commands accept only a JSON object, and `/connections` returns an array.
`/Accounts` also needs a tenant header and `accounting.settings.read`, and a
Custom Connection's token is minted by the Worker rather than stored by Dex
Web. Account codes, such as `090` for a bank account, are therefore
operation input, found in Xero under **Accounting > Chart of accounts**.

## Example

[`examples/approved-order`](examples/approved-order) is a runnable Dex Web
**Start Flow** example: for an approved and paid order it finds the
customer's contact by email, looks for an invoice already raised for the
order's reference, raises an approved invoice when there is none, records the
order's payment, and reads the invoice back to confirm it is paid.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the pinned Dex development server running, the example owns its real
Worker, retry, persistence, and duplicate-dispatch coverage:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```

From the repository root, check the manifest and generated code:

```bash
go run ./cmd/connectorctl validate connectors/xero/connector.yaml
go run ./cmd/connectorctl generate --check connectors/xero/connector.yaml
```

The provider fakes cover both token grants with HTTP Basic, scope checks,
token persistence and refresh after a 401, tenant selection by name, ID, or
the only organisation, exact decimal requests and responses, .NET dates,
paging and filters, the Idempotency-Key and its replay under a slow or lost
first request and a lost Worker, the minute and daily limits, a cached 500,
Xero error tokens without message text, redirects, and oversized, malformed,
and credential-reflecting responses.

No live Xero organisation was used. The following live behavior is
unverified: whether Xero's code exchange response includes `scope`; Xero's
handling of the form-body client secret that Dex Web sends; the
`X-Rate-Limit-Problem` value `day`, which only Xero's SDK messages suggest;
whether a Custom Connection token accepts or ignores `Xero-Tenant-Id`;
whether a 503 is cached under an idempotency key; JSON number formatting of
amounts in live responses; `If-Modified-Since` parsing; Xero's
`EmailAddress` filter with `==`; the developer portal labels in the setup
guidance; and the response to an invoice number reused by another invoice.
