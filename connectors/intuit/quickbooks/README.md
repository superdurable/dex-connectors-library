# QuickBooks Online Connector

> **Verification status: Dex-integrated, not live.** Every operation and the example ran on a real Dex stack against a local QuickBooks stand-in, and Dex Web loaded the local release and built the Intuit authorization URL. No live QuickBooks company or Intuit app was used. See
> [verification status](../../../docs/verification-status.md) for what the levels mean.

The QuickBooks Online Connector reads and writes the QuickBooks Online
Accounting API (`https://quickbooks.api.intuit.com/v3/company/{realmId}`, or
the sandbox host) from Dex Flows. It exposes these operation-specific Dex Step
factories:

| Operation | Kind | Happy branch | Other branches |
| --- | --- | --- | --- |
| `quickbooks.NewFindCustomerStep` | Query | `found` | `notFound`, `ambiguous`, `providerRejected`, `invalidResponse`, `defect` |
| `quickbooks.NewCreateCustomerStep` | Mutation | `created` | `nameConflict`, `providerRejected`, `uncertain`, `defect` |
| `quickbooks.NewCreateInvoiceStep` | Mutation | `created` | `providerRejected`, `uncertain`, `defect` |
| `quickbooks.NewGetInvoiceStep` | Query | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `quickbooks.NewListInvoicesStep` | Query | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `quickbooks.NewSendInvoiceStep` | Mutation | `sent` | `providerRejected`, `uncertain`, `defect` |
| `quickbooks.NewRecordPaymentStep` | Mutation | `recorded` | `providerRejected`, `uncertain`, `defect` |

Only the happy-path branch of each operation is required; every other branch
is optional, and an unwired optional branch fails the Flow. Every operation
uses async Execute durability, a 30-second Execute timeout, and a
five-minute retry window, which fits QuickBooks's documented one-minute wait
after a throttled request.

This release has no Triggers and no Studio pickers; see
[Not in this release](#not-in-this-release).

## Why these seven operations

They are the accounts-receivable loop a Process needs end to end, and each is
the ledger overlay's core verb with QuickBooks's own vocabulary:

- **Identity resolution:** `findCustomer` resolves a customer by email, the
  key other systems share, or by display name, the key QuickBooks keeps
  unique, and reports several matches instead of guessing. `createCustomer`
  creates the customer when there is none and reports a display name already
  in use on its own `nameConflict` branch, so a Flow can resolve it.
- **Document creation and numbering:** `createInvoice` raises an invoice with
  exact decimal lines and an optional document number, such as an order ID,
  which `listInvoices` can search before writing again.
- **Bounded reads:** `getInvoice` hydrates one invoice; `listInvoices` pages
  through invoices by customer, document number, open balance, invoice date,
  and last update, in a stable order with the next start position.
- **Delivery and settlement:** `sendInvoice` emails the invoice through
  QuickBooks, and `recordPayment` applies a received payment to it, after
  which the invoice's `Balance` shows what is still due.

Updates, bills, vendors, and period close are deliberately left for later
releases; see [Not in this release](#not-in-this-release).

## QuickBooks setup

A connection authorizes one QuickBooks Online company through Intuit's OAuth
2.0 consent. It needs an Intuit app with the QuickBooks Online Accounting API:

1. At `https://developer.intuit.com` choose **My Hub > App dashboard** and
   create an app, or open an existing one.
2. Under **Settings > Redirect URIs**, select **Development** or
   **Production**, choose **Add URI**, and add the Redirect URI that Dex Web
   shows. Intuit accepts `http://localhost` without HTTPS only for
   Development keys, which reach sandbox companies, and never an IP address,
   so open Dex Web at `http://localhost:<port>` rather than `127.0.0.1`.
   Production needs an `https` Redirect URI.
3. Under **Keys and credentials**, select the same keys, turn on **Show
   credentials**, and copy the Client ID and Client Secret into Dex Web.
   Production keys appear only after Intuit approves the app's Production Key
   questionnaire. Set `environment` to `sandbox` for Development keys.
4. Sign in to the QuickBooks Online company in the same browser, choose
   **Authorize**, and pick the company. Dex requests
   `com.intuit.quickbooks.accounting openid`.

Access tokens last one hour. The connector refreshes them five minutes before
they expire with Intuit's refresh token, presenting the app as HTTP Basic
credentials as Intuit's guide shows. Intuit replaces the refresh token at
least every 24 hours and expires the previous value when it does, so the
connector stores the refresh token from every response; project storage
admits one refresh at a time across every replica, because Intuit answers a
second refresh of the same token with `invalid_grant`. A refresh token
expires after 100 days without use and five years after consent; an
`invalid_grant` or `invalid_client` answer means the company must be
authorized again.

### The company's realm ID

Every QuickBooks request names the company in its path, as the realm ID, also
called the company ID. Intuit returns it as a `realmId` query parameter on
the OAuth redirect, which Dex Web does not keep. Intuit documents one other
source, the ID token, so the connector requests `openid`, maps the token
response's `id_token` to the `id_token` credential, and asks Intuit for the
claim with the authorization parameter `claims={"id_token":{"realmId":null}}`.
Before each call the connector reads the token's `realmid` claim (Intuit's
discovery document spells it `realmid` and its guides `realmId`; both are
read), after checking that the issuer is `https://oauth.platform.intuit.com/op/v1`
and that the audience is the connection's Client ID. The token came straight
from Intuit's token endpoint over TLS, which OpenID Connect Core 3.1.3.7
accepts in place of a signature check. The ID token from consent is kept
across refreshes, because the tokens stay bound to that company.

Intuit notes that the claim is populated only when the user is signed in to
QuickBooks while authorizing. For that case the connection has an optional
`realmId` field: the company ID from QuickBooks Online **Settings >
Subscriptions and billing**, or the Realm ID the OAuth 2.0 Playground shows
for a sandbox company. It is used only when the ID token names no company, and
a `realmId` that differs from the ID token's company is refused, so it can
never point the tokens at another company. A connection with neither selects
`defect` before any request.

### Dex Web authorization caveats

Dex Web exchanges the authorization code with `client_id` and
`client_secret` in a form body. Intuit's discovery document at
`https://developer.api.intuit.com/.well-known/openid_configuration` lists
both `client_secret_post` and `client_secret_basic`, so the form body is
accepted. Intuit's token response samples carry no `scope` field; Dex Web
`cli-v1.4.0` and later accept a response without one. Intuit documents no
PKCE, so the manifest declares none.

## Project configuration

Dex Web or Superverse Studio saves one connection record in the project
configuration. Its credentials hold `client_id`, `client_secret`, and the
`access_token`, `refresh_token`, and `id_token` from consent, and its
configuration may hold `environment`, `realmId`, and `maxResponseBytes`.

Load the project configuration and open the connection by the name the
application declares, as
[`examples/paid-order/main.go`](examples/paid-order/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := quickbooks.NewProjectConnection(project, paidorder.ConnectionName, connectionOptions()...)
```

`LoadFromEnvironment` reads the `DEX_PROJECT_*` environment described in
[project configuration](../../../sdkgo/projectconfig/README.md). Credentials
are read from project storage before every provider call, so a refreshed
token or a reauthorized company takes effect without a restart. The
environment, `realmId`, and response limit are startup configuration.

## Amounts, currencies, and QuickBooks values

Every amount is a `quickbooks.Decimal`, an exact base-10 string such as
`"112.40"`. The connector never converts one to floating point:

- an input is validated as a plain literal, without an exponent, a leading
  zero, or a thousands separator, and sent to QuickBooks as the same JSON
  number;
- a value QuickBooks returns keeps QuickBooks's digits, such as `0` or
  `99.90`; a number written with an exponent is expanded exactly;
- `IsZero`, `IsNegative`, and `IsPositive` compare without parsing.

Quantities and unit prices accept five decimal places; line and payment
amounts accept two, with at most ten digits before the point, inside
QuickBooks's documented 10.5 amount format. When a line leaves `amount` blank,
the connector computes quantity times unit price exactly and rounds half away
from zero to two places, so `3 x 0.33335` is `1.00`; a product with more
than ten digits before the point selects `defect` without a request. Amounts
are in the customer's currency: `currencyCode` is an ISO 4217 code that must
match the customer's when the company uses multicurrency.

QuickBooks's own vocabulary passes through unchanged. An invoice has no status
field: `Balance` is the amount still due and is zero when the invoice is
paid. `EmailStatus` is `NotSet`, `NeedToSend`, or `EmailSent`, line
`DetailType` is `SalesItemLineDetail`, `SubTotalLineDetail`, or another
QuickBooks value, and every Id is QuickBooks's decimal string. Times are
returned as UTC instants; QuickBooks writes them with the company's offset.
Every request sends `minorversion=75`, the version QuickBooks has used for
every request since it retired minor versions 1 to 74 on August 1, 2025.

## Operations

### findCustomer

`findCustomer` sends one query, such as
`select * from Customer where PrimaryEmailAddr = 'jane@example.com' MAXRESULTS 10`,
for exactly one of `emailAddress` or `displayName`
(`quickbooks.BuildFindCustomerQuery` returns it for review). String literals
are single-quoted with apostrophes escaped as `\'`, as QuickBooks's query
guide shows, and a backslash, a control character, or a malformed address
selects `defect` without a request. QuickBooks returns only active customers
unless the query adds `Active IN (true, false)`, which `includesInactive`
does. QuickBooks does not require email addresses to be unique, so the
connector keeps the customers whose address or display name equals the input
ignoring case, as QuickBooks compares, and selects `found` for one,
`notFound` for none, and `ambiguous` with up to ten `matches` for several.

### createCustomer

`createCustomer` sends `POST /customer` with a display name, which must be
unique across customers, vendors, and employees and cannot contain a colon,
plus optional email, given and family names, company, phone, and currency.
QuickBooks's error 6240, a display name already in use, selects
`nameConflict` with the requested name, so a Flow can call `findCustomer` by
display name.

### createInvoice, getInvoice, and listInvoices

`createInvoice` sends `POST /invoice` with the customer, 1 to 100 item lines
(each needs an item, because QuickBooks treats a line without one as text and
ignores its amount), dates, `docNumber`, `billEmail`, a customer memo, and a
private note. A `docNumber` with a backslash selects `defect`, because
`listInvoices` could not search for it. A blank `docNumber` lets QuickBooks
number the invoice when the company does not use custom transaction numbers;
with custom numbers on, a blank leaves the invoice unnumbered, and QuickBooks
rejects a number already used with error 6140, which selects
`providerRejected` with a `CONFLICT` Failure.

`getInvoice` reads `GET /invoice/{id}`; an answer about another invoice
selects `invalidResponse`, and error 610, Object Not Found, selects
`notFound` whether QuickBooks sends it with HTTP 400 or 200.

`listInvoices` sends one query with any of `customerId`, `docNumber`,
`isOpenOnly` (`Balance > '0'`), `txnDateFrom`, `txnDateTo`, and
`updatedSince` (`MetaData.LastUpdatedTime`), ordered by
`MetaData.CreateTime`, with `STARTPOSITION` and `MAXRESULTS` from 1 to 100
(`quickbooks.BuildListInvoicesQuery` returns it). QuickBooks returns no total
count, so a full page reports `hasMorePages` and the `nextStartPosition`.
QuickBooks does not document how it orders invoices created in the same
second, so a page boundary between them is not guaranteed stable.

### sendInvoice

`sendInvoice` sends `POST /invoice/{id}/send` with an empty
`application/octet-stream` body and an optional `sendTo`, which QuickBooks
also stores as the invoice's `BillEmail`. It selects `sent` only when the
returned invoice is marked `EmailSent`; any other accepted answer is
`uncertain`. QuickBooks sandbox companies send at most 40 emails a day.

### recordPayment

`recordPayment` sends `POST /payment` with the customer, the exact amount,
and one line that applies the whole amount to the invoice, plus an optional
date, reference number, deposit account, payment method, currency, and
private note. A blank deposit account deposits to QuickBooks's Undeposited
Funds account. It selects `recorded` only when the returned payment belongs
to the customer and applies to the invoice; read the invoice back with
`getInvoice` for its remaining `Balance`.

### Duplicate safety

QuickBooks documents the `requestid` query parameter for every request that
writes data: "instead of performing the operation again or returning an
error, it can recognize and send the same response for the original request",
for request IDs unique per company and at most 50 characters. All four
mutations send the Step's stable Call ID, a 36-character UUID, as
`requestid`, and their bodies are deterministic for one input, so every
attempt of one Step execution, including Dex's async backup dispatch and a
replay on a new Worker, sends the same request:

- a lost response, a timeout, HTTP 429, 408, or 5xx, or a `SystemFault` is
  retried with the identical request, and QuickBooks replays the first
  answer;
- error 600, Duplicate Request ID, is also retried. QuickBooks does not say
  when it sends 600 rather than the replay; the connector treats it as a
  repeat that arrived while the first request still ran, which a later
  attempt receives as the replay;
- an accepted 2xx that cannot be decoded, or that names another customer,
  invoice, or invoice application, selects `uncertain`. Read back with
  `listInvoices` by document number, `findCustomer`, or `getInvoice` before
  writing again with a new Step execution.

A new Step execution is a new logical write. Set `docNumber` to an
application ID, such as an order ID, and look it up with `listInvoices`
before creating, as the example does; Intuit recommends this client document
number plus a query before retry, because `DocNumber` is not itself an
idempotency key.

## Rate limits and errors

QuickBooks allows 500 requests a minute per company and 10 a second per
company and app, and answers more with HTTP 429. The connector retries after `Retry-After`,
or after the 60 seconds QuickBooks documents when no header is sent. Every
Receipt carries QuickBooks's `intuit_tid` as the provider request ID, and
`realmId` and `environment` in its metadata.

A Failure never repeats QuickBooks's `Message` or `Detail` text. It repeats
only the Fault type, the error codes, and the error count, such as
`QuickBooks rejected the request (HTTP 400) [ValidationFault; code 6000]`,
and the first code is in the Receipt metadata `faultCode`.

| Response | Result |
| --- | --- |
| 400 or 200 with Fault code 610 | `notFound` where declared, otherwise `providerRejected` with `NOT_FOUND` |
| 400 with code 6240 | `nameConflict` for `createCustomer`, `CONFLICT` |
| 400 with code 6140 | `providerRejected`, `CONFLICT` |
| 400 with code 600 | Retry under the same requestid |
| other 400 or 200 `ValidationFault` | `providerRejected`, `VALIDATION` |
| `SystemFault` in a 200 or 400 | Retry |
| 401 | `providerRejected`, `AUTHENTICATION`, after one token refresh and resend only when the stored token has expired |
| 403 | `providerRejected`, `AUTHORIZATION`; code 3100 names the missing scope |
| 404 | `notFound` where declared, otherwise `providerRejected` |
| 3xx | `providerRejected`, `PROTOCOL`; redirects are never followed |
| 429 | Retry after `Retry-After`, or 60 seconds |
| 408, 500, 502, 503, 504, transport failure | Retry |
| 501, other 4xx | `providerRejected` |
| oversized, malformed, or credential-reflecting 2xx | `invalidResponse`, or `uncertain` for a write |
| `invalid_grant` or `invalid_client` at the token endpoint | `providerRejected`, `AUTHENTICATION` |
| invalid input, or no known company | `defect`, with no request |

## Not in this release

### Triggers

QuickBooks webhooks need no handshake: Intuit signs each POST with
`intuit-signature`, an HMAC-SHA256 of the body under the app's verifier
token, and expects HTTP 200 within three seconds. They still do not fit
`sdkgo/webhooktrigger`, which decodes exactly one event per request: Intuit's
CloudEvents notification is a JSON array that can carry several entity
changes, for several companies, in one POST. The verifier token also belongs
to the app, not to one connection. A Trigger needs a multi-event decode in
`webhooktrigger`, with per-event IDs and per-company routing, and a
connection-independent verifier secret. The notification carries only entity
IDs, so a Flow would then call `getInvoice`. Until then, poll with
`listInvoices` and `updatedSince`; a durable poll Trigger on QuickBooks's
change data capture endpoint needs a per-binding cursor, which `sdkgo` does
not have yet.

### Updates

Updating a customer or invoice needs its `SyncToken`: QuickBooks rejects a
stale one with error 5010, Stale Object, and a full update clears every
omitted writable field. A later release can add sparse updates
(`"sparse": true`) that take the `syncToken` the reads already return and
report 5010 on a `conflict` branch.

### Studio pickers

Customer, item, account, and payment method pickers would query
`/v3/company/{realmId}/query`, but Studio setup commands use one fixed host,
and the host depends on the connection's environment. The realm ID also comes
from a credential claim, not a validated configuration field the command can
place in its path. Ids such as `itemId` and `depositAccountId` are therefore
operation input, read from QuickBooks's products and services list and chart
of accounts, or with a query.

## Example

[`examples/paid-order`](examples/paid-order) is a runnable Dex Web **Start
Flow** example: for an order the customer has already paid, it finds the
customer by email or creates them, looks for an invoice already numbered with
the order ID, raises one when there is none, records the payment, emails the
paid invoice as a receipt, and reads it back to confirm nothing is due.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the latest Dex development server running, the example owns its real
Worker, retry, persistence, and duplicate-dispatch coverage:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```

From the repository root, check the manifest and generated code:

```bash
go run ./cmd/connectorctl validate connectors/intuit/quickbooks/connector.yaml
go run ./cmd/connectorctl generate --check connectors/intuit/quickbooks/connector.yaml
```

The provider fakes cover the refresh-token grant with HTTP Basic, rotation,
a revoked grant, a 401 refreshed only once the recorded expiry has passed,
the realm from either claim spelling and the `realmId` fallback, the
environment hosts, `minorversion` on every request, exact decimal requests
and responses, query quoting, paging, the requestid and its replay under a
slow, lost, or duplicated first request and a lost Worker, throttling, Fault
codes without QuickBooks text, redirects, and oversized, malformed, and
credential-reflecting responses.

No live QuickBooks company was used. The following live behavior is
unverified: the Dex Web code exchange and its storage of `id_token`; whether
Intuit returns `realmid` in the ID token for every consent and an `id_token`
on refresh; whether the token response carries `scope`; how long QuickBooks
keeps a `requestid` and whether `/send` honors it; when QuickBooks answers
600 instead of replaying; the HTTP status of a missing invoice and of
error 5010; whether 429 carries `Retry-After`; the `PrimaryEmailAddr`
filter syntax; the ordering of invoices created in the same second; decimal
precision of `Qty`, `UnitPrice`, and `TotalAmt`, and the length limit of
`PaymentRefNum`; the length and character set of the Client ID and Client
Secret; and the developer portal labels in the setup guidance.
