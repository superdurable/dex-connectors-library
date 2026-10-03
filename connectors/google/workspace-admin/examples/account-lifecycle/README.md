# Google Workspace account-lifecycle example

This example runs one operation-only Flow from Dex Web **Start Flow**. The
`action` input selects one of three paths, and every path ends with an
`AccountHandOff` that tells the next owner what this Flow did not do.

Onboarding:

1. `RecordAccountRequest` validates the request and records it;
2. `CreateWorkspaceAccount` calls `workspaceadmin.NewCreateUserStep`. The
   generated password never leaves the request, and the creation key means a
   repeated Step finds its own account instead of creating a second one;
3. `RecordCreatedAccount` records the account, and `AddAccountToTeamGroup`
   calls `workspaceadmin.NewAddUserToGroupStep`, which reads an existing
   membership back instead of duplicating it;
4. `ReadBackWorkspaceAccount` calls `workspaceadmin.NewGetUserStep` with the
   member's unique ID, and `RecordOnboardingHandOff` confirms the account is the
   recorded one and active, then completes as `onboarded` with the sign-in
   hand-off;
5. when the address already belongs to another account, alias, or group,
   `RecordAddressTakenHandOff` completes as `addressTaken` without creating or
   joining anything.

Reinstating a returning person calls `workspaceadmin.NewUnsuspendUserStep`
instead of creating an account, then joins the same group, read-back, and
hand-off Steps and completes as `reinstated`.

Offboarding calls `workspaceadmin.NewSuspendUserStep`, removes the account from
the group by its unique ID with `workspaceadmin.NewRemoveUserFromGroupStep`, and
completes as `offboarded`.

Only the happy-path branches and `alreadyExists` are wired. An unknown account
to reinstate or offboard, a missing group, provider rejections, invalid
responses, and local defects fail the Flow.

## Configure

Follow the [Google Workspace Admin Connector setup](../../README.md), then
configure the `google-workspace-admin` connection under **Connections** in Dex
Web. Domain-wide delegation is the default method: paste the service-account
JSON key and enter the delegated administrator's address. No Step has
configuration of its own; the organizational unit and group come from each
Start Flow request.

## Run

Generate strict FDG 2.0 from `connectors/google/workspace-admin` with the latest
stable dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/account-lifecycle/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/account-lifecycle
```

Run `dexcli dev` with that build directory, then run the Worker with the
connection path shown by Dex Web:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/account-lifecycle
```

The default Worker address is `127.0.0.1:8845`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed.

Start `GoogleWorkspaceAccountLifecycle` with a unique Flow ID. Onboarding:

```json
{
  "action": "onboard",
  "primaryEmail": "ada.lovelace@example.com",
  "givenName": "Ada",
  "familyName": "Lovelace",
  "orgUnitPath": "/Engineering",
  "recoveryEmail": "ada@personal.example",
  "groupEmail": "engineering@example.com",
  "provisioningKey": "hire-2026-0042"
}
```

Reinstating or offboarding needs only the address and the group:

```json
{"action": "offboard", "primaryEmail": "ada.lovelace@example.com", "groupEmail": "engineering@example.com"}
```

`groupRole` may be `MEMBER`, `MANAGER`, or `OWNER`; blank joins as `MEMBER`.
Set `provisioningKey` to an HR hire ID so that a second onboarding run for the
same hire converges on the first account instead of reporting `addressTaken`.

The Flow result and the `google-workspace-account-hand-off` Attribute hold the
status, the account ID, the group outcome, and the next step. Creating an
account on a flexible Workspace plan bills a license, so run onboarding against
a test domain or an address you intend to keep.

## Test

```bash
GOWORK=off go test -race ./examples/account-lifecycle/...
```

With the latest Dex development server running, the integration test drives the
Flow on a real Worker against a stateful fake Directory API: an account insert
and a membership insert that each answer after nine seconds, so Dex dispatches
a backup attempt, with exactly one account and one membership created; a lost
insert response followed by two "User creation is not complete" membership
errors; an address that belongs to another account; a reinstated account that
keeps its group role; an offboarding whose first removal response is lost; an
unknown account on the unwired `notFound` branch; and an invalid request that
fails before any provider request. Every scenario that creates an account also
checks that no generated password reaches a Flow Attribute.

```bash
GOWORK=off go test -tags=integration ./examples/account-lifecycle/... -count=1 -v
```
