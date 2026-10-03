# BambooHR new-hire onboarding example

This example runs one operation-only Flow from Dex Web **Start Flow**. It checks
one new hire's BambooHR record and records the hand-off to IT, which provisions
the hire's accounts:

1. `RecordNewHire` validates the hire's name, personal email, hire date, and
   the alias of the BambooHR custom text field that records the hand-off, and
   records the hire.
2. `FindNewHire` calls `bamboohr.NewFindEmployeeByEmailStep` on the personal
   email, BambooHR's `homeEmail`, before anything is added, so a re-run finds
   the hire instead of adding a second record. `AdoptExistingEmployee` uses the
   one match; `RecordAmbiguousHire` completes for review when several
   employees share the address.
3. When nobody has the address, `PrepareNewHireRecord` and `AddNewHire`
   (`bamboohr.NewAddEmployeeStep`) add the hire with name, personal email, and
   hire date, and `AdoptAddedEmployee` records the new employee ID.
4. `ReadNewHireRecord` calls `bamboohr.NewGetEmployeeStep` for the fields IT
   needs, `department`, `jobTitle`, `location`, `supervisorEId`, and
   `hireDate`, with `status`, both emails, and the hand-off field.
5. `CheckOnboardingReadiness` completes as `alreadyHandedOff` when the
   hand-off field already holds a value, and as `held` with `missingFields`
   when a required field is empty, not visible to the connection, or the hire
   is not `Active`. Nothing is written in either case; HR completes the record
   and starts the Flow again.
6. Otherwise `ListStartWindowTimeOff` calls
   `bamboohr.NewListTimeOffRequestsStep` for approved or requested time off in
   the hire's first two weeks, and `DecideProvisioningHandoff` composes the
   hand-off text from the record and that time off.
7. `RecordProvisioningHandoff` calls `bamboohr.NewUpdateEmployeeStep` to store
   the text in the custom field, and `CompleteOnboardingCheck` completes as
   `handedOff`, or for review when BambooHR stored a different value.

The `uncertain` branch of the add is wired: `RecordUncertainHire` records why
the outcome is unknown, `ReconcileUncertainHire` looks the hire up by personal
email, and `AdoptReconciledEmployee` continues with the one employee it finds.
When it finds none or several, `RecordUnreconciledHire` completes with
`needsReview: true`; the Flow never adds the hire a second time. Every other
optional branch is unwired and fails the Flow, such as a rejected add, a
missing employee, an invalid response, or a local defect.

In production, start one Flow per hire with a stable Flow ID such as
`onboarding-{personal email}` and the Dex ID reuse policy that fits your
re-run rule, so two concurrent starts for the same person cannot both add
them; Flow IDs cannot contain `:`. The
[employee change sweep](../employee-change-sweep) lists the employees added
since a cursor, which is how a scheduler finds hires to check.

## Prepare BambooHR

Follow the [BambooHR setup](../../README.md#bamboohr-setup). The hand-off field
is a custom text field on the employee record, such as **IT Provisioning**
with the alias `customITProvisioning`; BambooHR's List Fields endpoint,
`GET /api/v1/meta/fields`, shows each field's alias. The connection's user
needs to view and edit that field, view the provisioning fields, add
employees, and view the hire's time off.

## Generate the Flow Definition

Generate strict FDG 2.0 from `connectors/bamboohr` with the latest stable dexcli
release:

```bash
mkdir -p build
dexcli visualize ./examples/new-hire-onboarding/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/new-hire-onboarding
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
mkdir -p /tmp/bamboohr-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/bamboohr/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/bamboohr \
  --version v0.1.0 --tag connectors/bamboohr/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/bamboohr-release/connector-release.json \
  --digest-output /tmp/bamboohr-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/bamboohr/build" \
  --connector-config-dir "$HOME/.dex/connectors" \
  --connector-release-override bamboohr=/tmp/bamboohr-release
```

Open **Connections** in Dex Web and select `bamboohr / bamboohr-company`, which
every BambooHR Step of `BambooHRNewHireOnboarding` uses. Enter the company
domain and the API key and save; the status becomes **Ready**. The example has
no Step configuration. The key is stored only in the plaintext development
file shown on the page; never commit or share it.

In a second terminal, start the Worker from `connectors/bamboohr` with that
file:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/new-hire-onboarding
```

The Worker reads the connection at startup and the credentials before every
call, so restart it after changing the company domain. The default Worker
address is `127.0.0.1:8836`. Override `DEX_FLOW_SERVICE_ADDRESS`,
`DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR` when needed. For local
verification against a BambooHR-compatible fake only,
`BAMBOOHR_LOCAL_API_BASE_URL` replaces
`https://{companyDomain}.bamboohr.com/api/v1`; it must be HTTPS or a loopback
HTTP URL, and production Workers leave it unset.

In the Run workspace, choose **Start Flow**, select
`BambooHRNewHireOnboarding`, choose the Worker at `127.0.0.1:8836`, enter a
unique Flow ID, and submit:

```json
{
  "firstName": "Ava",
  "lastName": "Nguyen",
  "personalEmail": "ava.nguyen@personal.example.com",
  "hireDate": "2026-10-13",
  "handoffFieldName": "customITProvisioning"
}
```

`firstName`, `lastName`, and `hireDate` are used only when BambooHR has no
employee with the personal email. The Flow result and the
`bamboohr-onboarding-outcome` Attribute hold the action (`handedOff`,
`alreadyHandedOff`, `held`, or `needsReview`), the employee ID, the missing
fields, the start-window time off, the hand-off text, and the review state.
Running the example adds an employee to BambooHR when none has the personal
email; use a sandbox company or remove the test employee afterwards.

## Test

```bash
GOWORK=off go test -race ./examples/new-hire-onboarding/...
```

With the latest Dex development server running, the integration tests drive
the Flow on a real Worker against a stateful fake BambooHR that, like
BambooHR, has no idempotency key and matches emails by substring:

- a new hire is added once and held for missing fields; once HR completes the
  record, a re-run finds the hire by personal email, adds nobody, and stores
  the hand-off, while a look-alike personal email is left alone;
- an existing hire is handed off with only their approved or requested time
  off in the first two weeks, and a second run finds the hand-off recorded and
  writes nothing;
- an add held for nine seconds, past Dex's async local phase, is sent exactly
  once, because the Step is sync;
- an update held for nine seconds is dispatched again by async Dex, and both
  attempts store the same text;
- a Worker lost while BambooHR holds the add is replaced, and the new attempt
  finds the dispatch checkpoint, sends nothing, and adopts the hire it finds
  by personal email;
- a lost add response is reconciled the same way and never resent;
- an add that fails with 500 without being applied completes as
  `needsReview` and is never resent;
- a rate-limited add waits for `Retry-After` and adds one employee;
- a rejected add fails the Flow through the unwired `providerRejected` branch
  without BambooHR's message text;
- two employees sharing the personal email, and an unknown hand-off field,
  complete as `needsReview` without writing;
- an invalid hire date fails the Flow before any BambooHR request.

```bash
GOWORK=off go test -tags=integration ./examples/new-hire-onboarding/... -count=1 -v
```
