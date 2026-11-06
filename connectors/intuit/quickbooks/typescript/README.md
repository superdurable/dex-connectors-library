# QuickBooks Online TypeScript connector

`@superdurable/connector-intuit-quickbooks` supplies seven QuickBooks Online
operations for Dex TypeScript Flows. `connector.yaml` generates the models,
validators, Step factories, OAuth refresh declaration, named connection, and
scripted mocks. The package implements the provider client and operation
invocations. Amounts are exact base-ten strings in the public models and are
sent as JSON number literals to QuickBooks.

## Connect

Install the connector, `@superdurable/connectors-sdk` 0.5.0, and
`@superdurable/dex` 1.28.0 or later on Node.js 22 or 24. Configure the
`quickbooks-accounting` connection in Dex Web with an Intuit app and the
QuickBooks Online Accounting API. Follow the authorization guide in the
manifest: add Dex Web's exact redirect URI in the Intuit app, select the
matching Development or Production keys, and authorize the company. Dex
requests `com.intuit.quickbooks.accounting` and `openid`. The access token,
rotating refresh token, client secret, and ID token stay in host-owned
credentials, outside Flow state and output.

At Worker startup, call `loadFromEnvironment()` and bind the static
`ConnectionName` with `newProjectConnection(project, ConnectionName)`. The
[checked-in example](examples/get-invoice/) is the runnable application. The
client reads `environment`, `realmId`, and `maxResponseBytes` from the project
connection. If Intuit's ID token names a company, its realm ID must agree with
an explicitly configured `realmId`.

## Operations

| Factory | Provider call | Happy branch |
| --- | --- | --- |
| `newFindCustomerStep` | Query Customer by email or display name | `found` |
| `newCreateCustomerStep` | `POST /customer` | `created` |
| `newCreateInvoiceStep` | `POST /invoice` | `created` |
| `newGetInvoiceStep` | `GET /invoice/{id}` | `found` |
| `newListInvoicesStep` | Query a bounded Invoice page | `listed` |
| `newSendInvoiceStep` | `POST /invoice/{id}/send` | `sent` |
| `newRecordPaymentStep` | `POST /payment` | `recorded` |

All other declared branches are optional; an unwired optional branch fails
the Flow. Mutations use the Dex Call ID as QuickBooks `requestid` across
attempts. An accepted write with an unusable response is read back by
provider ID when possible; otherwise it selects `uncertain`.

## Verify

From this package run `npm ci`, `npm run check`, and, with `dexcli dev` and
`DEX_FLOW_SERVICE_ADDRESS` set, `npm run test:integration`. The integration
test uses a loopback QuickBooks stand-in and a real Dex Worker and Client.
The shared Go-recorded conformance fixture covers provider branches, retries,
transport failures, stable request IDs, and accepted-write read-back. Live
Intuit app authorization, token refresh, realm claim delivery, and QuickBooks
API calls remain unverified.
