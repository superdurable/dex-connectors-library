# Xero approved-order example

This example runs one operation-only Flow from Dex Web **Start Flow**. For an
approved order the customer has already paid, it books the sale in Xero once:

1. `RecordApprovedOrder` validates the order ID, customer email, currency,
   dates, lines, and payment account, and records the order;
2. `FindCustomerContact` calls `xero.NewFindContactByEmailStep`. When no
   active contact has the email, `CompleteWithoutContact` completes as
   `contactMissing` without writing;
3. `PrepareOrderInvoiceLookup` records the contact, and `FindOrderInvoice`
   calls `xero.NewListInvoicesStep` for the customer's `AUTHORISED` or `PAID`
   sales invoices whose reference is the order ID;
4. `ChooseOrderInvoice` completes as `alreadySettled` when that invoice is
   paid, pays an approved one, or otherwise goes on to raise one;
5. `RaiseOrderInvoice` calls `xero.NewCreateInvoiceStep` for an `AUTHORISED`
   `ACCREC` invoice with the order's exact line amounts, currency, dates, and
   the order ID as reference, and lets Xero number it;
6. `PrepareOrderPayment` takes the exact `amountDue` Xero returned, and
   `RecordOrderPayment` calls `xero.NewRecordPaymentStep` from the receiving
   bank account;
7. `ReadBackPaidInvoice` calls `xero.NewGetInvoiceStep`, and
   `CompleteInvoicedOrder` completes as `invoicedAndPaid` or
   `existingInvoicePaid`, with `isFullyPaid` from the invoice as read back.

Only the happy-path branches and `notFound` are wired. An ambiguous contact, a
rejected write, the daily limit, an `uncertain` write, an invalid response,
and a local defect fail the Flow. Both writes send Xero an Idempotency-Key
derived from the Step, so a retried or re-dispatched Step receives the first
invoice or payment instead of writing again.

## Generate the Flow Definition

Generate strict FDG 2.0 from `connectors/xero` with the latest stable dexcli
release:

```bash
dexcli visualize ./examples/approved-order/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/xero-approved-order
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
mkdir -p /tmp/xero-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/xero/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/xero \
  --version v0.1.0 --tag connectors/xero/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/xero-release/connector-release.json \
  --digest-output /tmp/xero-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir /tmp \
  --connector-config-dir "$HOME/.dex/connectors" \
  --connector-release-override xero=/tmp/xero-release
```

Follow the [Xero setup](../../README.md#xero-setup), then open
**Connections** in Dex Web and select `xero / xero-books`, which all five
Xero Steps of `XeroApprovedOrderInvoice` use. Choose **Custom Connection**
and enter the client ID and secret, or **Xero OAuth 2.0 web app** and
authorize, and save. The example has no Step configuration. Secrets are
stored only in the plaintext development file shown on the page; never
commit or share it.

In a second terminal, start the Worker from `connectors/xero` with that file:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/approved-order
```

The Worker reads the connection at startup and the credentials before every
call. The default Worker address is `127.0.0.1:8836`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed. For local verification against a Xero-compatible fake only,
`XERO_LOCAL_PROVIDER_URL` sends every request for `api.xero.com` and
`identity.xero.com` to one loopback URL without a path; production Workers
leave it unset.

In the Run workspace, choose **Start Flow**, select
`XeroApprovedOrderInvoice`, choose the Worker at `127.0.0.1:8836`, enter a
unique Flow ID, and submit. The customer must already be a Xero contact with
that email. `accountCode` is a sales account and `paymentAccountCode` a bank
account from **Accounting > Chart of accounts**; the Demo Company has `200`
(Sales) and `090` (Business Bank Account). Amounts are exact decimal strings
excluding tax.

```json
{
  "orderId": "ORDER-1042",
  "customerEmail": "accounts@cityagency.example.com",
  "currencyCode": "USD",
  "invoiceDate": "2026-10-01",
  "dueDate": "2026-10-15",
  "lines": [
    {"description": "Spring bouquet", "quantity": "2", "unitAmount": "49.95", "accountCode": "200"},
    {"description": "Delivery", "quantity": "1", "unitAmount": "12.50", "accountCode": "200"}
  ],
  "paidOn": "2026-10-01",
  "paymentAccountCode": "090",
  "paymentReference": "card charge 1042"
}
```

Omit `taxType` to use each account's default tax type. The Flow result and
the `xero-invoiced-order-outcome` Attribute hold the action, the invoice ID,
number, status, total, and amount due, and the payment ID. The run writes to
the real organisation; void the test invoice in Xero afterwards.

## Test

```bash
GOWORK=off go test -race ./examples/approved-order/...
```

With the latest Dex development server running, the integration tests drive
the Flow on a real Worker against a stateful fake Xero that issues Custom
Connection tokens, lists connections, and, like Xero, caches each
Idempotency-Key's response, makes a concurrent repeat wait for the first
request, and replays it:

- a new order raises one invoice with exact number literals and the order
  reference, records one payment of the exact total, and reads back `PAID`;
  the Custom Connection token is requested once with HTTP Basic, persisted,
  and sent without a tenant header;
- an invoice create and a payment each held for nine seconds, past Dex's
  async local phase, are dispatched again; the repeat receives the replay,
  so Xero holds one invoice and one payment;
- a lost create response is retried under the same key and replayed;
- a Worker lost while Xero holds the payment is replaced, and the new
  Worker's attempt sends the same key and records no second payment;
- a paid invoice for the order is left alone, an approved one is paid without
  raising another, and another customer's invoice with the same reference is
  ignored;
- a missing contact completes without writing;
- a minute limit waits for `Retry-After`;
- the daily limit, a validation rejection, and a cached 500 each fail the
  Flow through an unwired optional branch after one request, without Xero's
  message text;
- an OAuth connection sends the tenant of the named organisation, read once
  from `/connections`;
- an invalid email fails the Flow before any Xero request.

```bash
GOWORK=off go test -tags=integration ./examples/approved-order/... -count=1 -v
```
