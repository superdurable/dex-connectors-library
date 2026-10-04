# QuickBooks paid-order example

This example runs one operation-only Flow from Dex Web **Start Flow**. For an
order the customer has already paid, it books the sale in QuickBooks Online
once:

1. `RecordPaidOrder` validates the order ID, customer, dates, lines, and
   payment details, and records the order;
2. `FindOrderCustomer` calls `quickbooks.NewFindCustomerStep` with the
   customer's email. When no active customer has it,
   `PrepareCustomerCreation` and `CreateOrderCustomer`
   (`quickbooks.NewCreateCustomerStep`) create the customer with the order's
   display name; `RecordFoundCustomer` or `RecordCreatedCustomer` records the
   customer;
3. `FindOrderInvoice` calls `quickbooks.NewListInvoicesStep` for the
   customer's invoices numbered with the order ID;
4. `ChooseOrderInvoice` completes as `alreadySettled` when that invoice has no
   balance, pays an open one, or otherwise goes on to raise one;
5. `RaiseOrderInvoice` calls `quickbooks.NewCreateInvoiceStep` with the order
   ID as the invoice number, the order's exact lines, dates, and the
   customer's email as `BillEmail`;
6. `PrepareOrderPayment` takes the exact `Balance` QuickBooks returned, and
   `RecordOrderPayment` calls `quickbooks.NewRecordPaymentStep`;
7. `PrepareInvoiceDelivery` and `SendPaidInvoice`
   (`quickbooks.NewSendInvoiceStep`) email the paid invoice to the customer as
   a receipt;
8. `ReadBackPaidInvoice` calls `quickbooks.NewGetInvoiceStep`, and
   `CompletePaidOrder` completes as `invoicedAndPaid` or
   `existingInvoicePaid`, with `isFullyPaid` from the invoice as read back.

Only the happy-path branches and `notFound` are wired. An ambiguous email, a
display name in use, a rejected write, an `uncertain` write, an invalid
response, and a local defect fail the Flow. Every write sends QuickBooks a
`requestid` derived from the Step, so a retried or re-dispatched Step receives
the first customer, invoice, payment, or delivery instead of writing again.

## Generate the Flow Definition

Generate strict FDG 2.0 from `connectors/intuit/quickbooks` with the latest
stable dexcli release:

```bash
dexcli visualize ./examples/paid-order/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/quickbooks-paid-order
```

The command must report `valid: true`. Inside this repository it also warns
`connector_release_required` for each connector Step, because a local module
is not a published release.

## Configure and run

This example is part of the connector module, so Dex needs release metadata
built from this source, passed as an override; without it the connection
shows **Unsupported**. The connector has no Studio bundle. From the repository
root:

```bash
cd "$(git rev-parse --show-toplevel)"
mkdir -p /tmp/quickbooks-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/intuit/quickbooks/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks \
  --version v0.21.0 --tag connectors/intuit/quickbooks/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/quickbooks-release/connector-release.json \
  --digest-output /tmp/quickbooks-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir /tmp \
  --connector-release-override quickbooks=/tmp/quickbooks-release
```

Follow the [QuickBooks setup](../../README.md#quickbooks-setup) with
Development keys and a sandbox company, open Dex Web at
`http://localhost:<port>`, then open **Connections** and select
`quickbooks / quickbooks-company`, which all seven QuickBooks Steps of
`QuickBooksPaidOrderInvoice` use. Enter the Client ID and Client Secret, set
`environment` to `sandbox`, sign in to the sandbox company in the same
browser, and choose **Authorize**. The example has no Step configuration.

The lookup finds an earlier invoice by its number, so turn on custom
invoice numbers in the company: in QuickBooks Online choose **Settings >
Account and settings > Sales**, open **Sales form content**, turn on
**Custom transaction numbers**, and save. Without it QuickBooks numbers
invoices itself and a rerun of the same order raises a second invoice.

In a second terminal, start the Worker from `connectors/intuit/quickbooks`.
It reads the `DEX_PROJECT_*` project configuration environment described in
[project configuration](../../../../../sdkgo/projectconfig/README.md); Dex Web
or Superverse Studio writes that configuration when you save the connection.

```bash
go run ./examples/paid-order
```

The Worker reads the connection at startup and the credentials before every
call. The default Worker address is `127.0.0.1:8881`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed. For local verification against a QuickBooks-compatible fake only,
`QUICKBOOKS_LOCAL_PROVIDER_URL` sends every request for the QuickBooks API
hosts and `oauth.platform.intuit.com` to one loopback URL without a path;
production Workers leave it unset.

In the Run workspace, choose **Start Flow**, select
`QuickBooksPaidOrderInvoice`, choose the Worker at `127.0.0.1:8881`, enter a
unique Flow ID, and submit. `itemId` is the Id of a product or service in the
company, and `depositAccountId` the Id of a bank account; leave it out to use
Undeposited Funds. Prices are exact decimal strings, and the order ID is at
most 21 characters because it becomes the invoice number.

```json
{
  "orderId": "ORDER-1042",
  "customerEmail": "accounts@cityagency.example.com",
  "customerDisplayName": "City Agency",
  "customerCompanyName": "City Agency LLC",
  "invoiceDate": "2026-10-01",
  "dueDate": "2026-10-15",
  "lines": [
    {"itemId": "1", "description": "Spring bouquet", "quantity": "2", "unitPrice": "49.95", "taxCodeId": "NON"},
    {"itemId": "1", "description": "Delivery", "quantity": "1", "unitPrice": "12.50"}
  ],
  "paidOn": "2026-10-01",
  "paymentReference": "ch_3QxF2a"
}
```

The Flow result and the `quickbooks-paid-order-outcome` Attribute hold the
action, whether the customer was created, the invoice ID, number, total,
balance, and email status, and the payment ID. The run writes to the company
and emails the customer address; use a sandbox company and an address you
own, and delete the test transactions in QuickBooks afterwards.

## Test

```bash
GOWORK=off go test -race ./examples/paid-order/...
```

With the latest Dex development server running, the integration tests drive
the Flow on a real Worker against a stateful fake QuickBooks that refreshes
and rotates tokens, checks the realm in every path and `minorversion=75` on
every request, and, like QuickBooks, replays the original response to a
repeated `requestid`; while the first request still runs it answers a repeat
with error 600, which the connector retries:

- a new order creates one customer, raises one invoice with exact number
  literals and the order ID as its number, records one payment of the exact
  balance, sends the invoice once, and reads back a zero balance and
  `EmailSent`; the expired access token is refreshed once with HTTP Basic and
  the rotated refresh token is stored;
- an existing active customer is found by email, past an inactive customer
  and a look-alike address;
- an invoice create and a payment each held for nine seconds, past Dex's
  async local phase, are dispatched again; the repeat is refused with 600,
  retried, and replayed, so QuickBooks holds one invoice and one payment;
- a lost create response is retried under the same `requestid` and replayed;
- a Worker lost while QuickBooks holds the payment is replaced, and the new
  Worker's attempt sends the same `requestid` and records no second payment;
- a paid invoice for the order is left alone, and an open one is paid without
  raising another while another customer's invoice with the same number is
  ignored;
- two customers sharing the email, a display name a vendor already uses, a
  business-rule rejection, and a revoked refresh token each fail the Flow
  through an unwired optional branch, without QuickBooks's text or a retry;
- a throttled lookup waits for `Retry-After`;
- an order ID longer than an invoice number fails the Flow before any
  QuickBooks request.

```bash
GOWORK=off go test -tags=integration ./examples/paid-order/... -count=1 -v
```
