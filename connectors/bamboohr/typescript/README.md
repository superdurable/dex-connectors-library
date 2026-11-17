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

Use Node.js 22 or 24 and Dex TypeScript SDK 1.45.4 or later, which
`@superdurable/connectors-sdk` 0.12.0 requires. In Dex Web
Connections, set `companyDomain` to the part before `.bamboohr.com` in the
company URL. Sign in at https://app.bamboohr.com/login/, open your name's menu
in the lower left of the BambooHR page, choose **API Keys**, create a key, and
save it in the private `api_key` field. See the [connector setup
instructions](../README.md#bamboohr-setup) for account permission and key
handling details. Leave `maxResponseBytes` blank to use the manifest safety
limit. Leave `pollInterval` blank to keep the displayed delay between
`employeeChanged` polls, or enter a Go duration of at least `10s`. The package resolves the current key before every API request and sends
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
It reads back a known accepted ID, resends an unknown outcome, such as a 5xx,
409, or lost response, while possible sends stay below the Step's send cap, and
returns `uncertain` at the cap or when an accepted write cannot be confirmed.
Version `0.2.1` adds that bounded resend, returns Retry for an unreadable
dispatch checkpoint, and retries an `updateEmployee` write that BambooHR did
not definitely refuse. Neither mutation sends an idempotency header; its Dex
Call ID correlates the receipt.

## employeeChanged Trigger

Version `0.2.0` adds the `employeeChanged` poll Trigger on the
`@superdurable/connectors-sdk` 0.12.0 poll Trigger kit, at behavior parity with
the Go connector. `newProjectEmployeeChangedTriggerRunner` loads each saved
binding's change type (an unsaved binding delivers every change) and records
each change in the binding's project Trigger inbox before its cursor commits;
`newEmployeeChangedTriggerRunner` takes routes with targets and cursor stores;
`newEmployeeChangedTrigger` keeps the cursor in memory, as Go's generated
factory does. Declare the binding with `defineEmployeeChangedTriggerBinding`
and the `changeTypePicker` unit (`UIUnitChangeTypePicker`), bound to
`/changeType`.

Each poll makes one `GET /employees/changed` request, delivers changes at
least 30 seconds old by BambooHR's Date header, oldest first by `lastChanged`
and numeric employee ID, at most 500 per page, and names each event
`employee-<id>-<action>-<lastChanged Unix seconds>`. The payload is the Go
JSON `{employeeId, action, lastChanged}`. A 429 or 503 pauses every binding of
the connection; a 400 rejecting the cursor starts the binding again at the
current time. The cursor document is the Go connector's byte for byte, so Go
and TypeScript replicas share it. `BambooHRClient` rejects a `pollIntervalMs`
under 10 seconds. The [employee-changed example](examples/employee-changed/)
starts one Flow per change and reads inserted and updated employees back.

## Verify

```sh
npm ci
npm run check
```

With `dexcli dev` running and `DEX_FLOW_SERVICE_ADDRESS` set, run
`npm run test:integration`. The tests run the checked-in Flows through a real
Dex Worker and Client against local BambooHR stand-ins, including an unwired
authorization failure, employee changes across a Trigger restart, a saved
change type, and a reseed after a rejected cursor. The Go-recorded provider
scenarios compare requests, outputs, branches, retries, uncertainty, and
mutation keys, and `test/employee_changed_go_parity.test.ts` replays a
recording of the Go employeeChanged fetcher's cursors, events, and errors.

Live BambooHR authorization, company-domain routing, field visibility, employee
writes, time off data, live polling, and the Dex Web connection form have not
been verified.
