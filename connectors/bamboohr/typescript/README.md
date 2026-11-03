# BambooHR TypeScript connector

`@superdurable/connector-bamboohr` supplies `getEmployee`, `findEmployeeByEmail`,
`listEmployeeChanges`, `listTimeOffRequests`, `updateEmployee`, and `addEmployee`
for Dex TypeScript Flows. Its Step factories, models, named connection, and mocks
come from [`connector.yaml`](../connector.yaml) and the Go model types. Regenerate
from the repository root with `go run ./cmd/connectorctl generate --lang typescript
connectors/bamboohr/connector.yaml`.

## Install and connect

```sh
npm install @superdurable/connector-bamboohr @superdurable/connectors-sdk @superdurable/dex
```

Use Node.js 22 or 24 and Dex TypeScript SDK 1.28.0 or later. In Dex Web
Connections, set `companyDomain` to the part before `.bamboohr.com` in the
company URL. Sign in at https://app.bamboohr.com/login/, open your name's menu
in the lower left of the BambooHR page, choose **API Keys**, create a key, and
save it in the private `api_key` field. See the [connector setup
instructions](../README.md#bamboohr-setup) for account permission and key
handling details. Leave `maxResponseBytes` blank to use the manifest safety
limit. The package resolves the current key before every API request and sends
it as the HTTP Basic user name with `x` as the password.

At Worker startup, call `loadFromEnvironment()` and pass the static
`ConnectionName` to `newProjectConnection`, as the [runnable
example](examples/get-employee/) does. Flow input contains employee IDs and
field names, never a key.

## Operations

| Factory | Request | Happy branch |
| --- | --- | --- |
| `newGetEmployeeStep` | `GET /employees/{id}` with requested fields | `found` |
| `newFindEmployeeByEmailStep` | `GET /employees` with an email filter | `found` |
| `newListEmployeeChangesStep` | `GET /employees/changed` | `listed` |
| `newListTimeOffRequestsStep` | `GET /time-off/requests` | `listed` |
| `newUpdateEmployeeStep` | Read, absolute field write, read-back | `updated` |
| `newAddEmployeeStep` | Fenced `POST /employees` | `created` |

Every other declared branch is optional. An unwired optional branch fails the
Flow. `updateEmployee` reads before writing, sends only changed plain field
values, and reads back; repeated writes are safe. `addEmployee` uses the SDK's
single-dispatch fence because BambooHR documents no provider idempotency key.
It retries only a confirmed non-application, reads back a known accepted ID,
and returns `uncertain` when an accepted write cannot be confirmed. Neither
mutation sends an idempotency header; its Dex Call ID correlates the receipt.

## Verify

```sh
npm ci
npm run check
```

With `dexcli dev` running and `DEX_FLOW_SERVICE_ADDRESS` set, run
`npm run test:integration`. The test runs the checked-in Flow through a real
Dex Worker and Client against a local BambooHR HTTP fixture, including an
unwired authorization failure. The Go-recorded provider scenarios compare
requests, outputs, branches, retries, uncertainty, and mutation keys.

Live BambooHR authorization, company-domain routing, field visibility, employee
writes, time off data, and the Dex Web connection form have not been verified.
