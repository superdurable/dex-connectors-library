# Microsoft Entra ID account-lifecycle example

This example runs one operation-only Flow, `EntraAccountLifecycle`, from Dex
Web **Start Flow**. The `action` input selects one of three paths, and every
path ends with an `AccountHandOff` that tells the next owner what this Flow
did not do.

Onboarding:

1. `RecordAccountRequest` validates the request and records it;
2. `CreateEntraAccount` calls `entraid.NewCreateUserStep`. The generated
   password never leaves the request, and the creation key means a repeated
   Step, or a later run with the same `provisioningKey`, finds its own account
   instead of creating a second one;
3. `RecordCreatedAccount` records the account, and `AddAccountToTeamGroup`
   calls `entraid.NewAddUserToGroupStep`, which confirms an existing membership
   instead of failing;
4. `RecordOnboardingHandOff` completes as `onboarded` with the sign-in
   hand-off;
5. when the name already belongs to another account, or a deleted one,
   `RecordAddressTakenHandOff` completes as `addressTaken` without creating or
   joining anything.

Reinstating a returning person calls `entraid.NewEnableUserStep` instead of
creating an account, then joins the same group and hand-off Steps and
completes as `reinstated`.

Offboarding calls `entraid.NewGetUserStep` to find the account, then
`entraid.NewDisableUserStep` by its object ID, then
`entraid.NewRevokeSignInSessionsStep`, then removes the account from the group
with `entraid.NewRemoveUserFromGroupStep`, and completes as `offboarded`.

Only the happy-path branches and `alreadyExists` are wired. An unknown account
to reinstate or offboard, a missing group, provider rejections, invalid
responses, and local defects fail the Flow.

## Generate the Flow Definition

Generate strict FDG 2.0 from `connectors/microsoft/entra-id` with the latest
stable dexcli release:

```bash
dexcli visualize ./examples/account-lifecycle/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/entra-id-account-lifecycle
```

The command must report `valid: true`. Inside this repository it also warns
`connector_release_required` for each connector Step, because a local module
is not a published release.

## Configure and run

This example is part of the connector module, so Dex needs release metadata
built from this source, passed as an override. From the repository root:

```bash
mkdir -p /tmp/entra-id-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/microsoft/entra-id/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id \
  --version v0.21.0 --tag connectors/microsoft/entra-id/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/entra-id-release/connector-release.json \
  --digest-output /tmp/entra-id-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir /tmp \
  --connector-release-override microsoft-entra-id=/tmp/entra-id-release
```

Follow the [Microsoft Entra ID setup](../../README.md#authorization), then open
**Connections** in Dex Web and select `microsoft-entra-id / microsoft-entra-id`,
which all seven connector Steps use. Choose **App-only access** and enter the
tenant ID, client ID, and client secret, or **Microsoft OAuth as an
administrator** and authorize (Dex CLI 1.4.1 or later). No Step has
configuration of its own; the account and group come from each Start Flow
request.

In a second terminal, start the Worker from `connectors/microsoft/entra-id`. It
reads the `DEX_PROJECT_*` project configuration environment documented in
[`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md#application-loading);
Dex Web or Superverse Studio writes that configuration:

```bash
go run ./examples/account-lifecycle
```

The default Worker address is `127.0.0.1:8849`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed. For local verification against a Microsoft-compatible fake only,
`ENTRA_ID_LOCAL_PROVIDER_URL` sends every request for `graph.microsoft.com` and
`login.microsoftonline.com` to one loopback URL without a path.

Start `EntraAccountLifecycle` with a unique Flow ID. `groupId` is the object ID
shown at Microsoft Entra admin center > Groups > the group > Overview; it must
be a security or Microsoft 365 group with assigned membership. Onboarding:

```json
{
  "action": "onboard",
  "userPrincipalName": "ada.lovelace@contoso.com",
  "givenName": "Ada",
  "surname": "Lovelace",
  "department": "Engineering",
  "usageLocation": "GB",
  "groupId": "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
  "provisioningKey": "hire-2026-0042"
}
```

Reinstating or offboarding needs only the name and the group:

```json
{"action": "offboard", "userPrincipalName": "ada.lovelace@contoso.com", "groupId": "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"}
```

The Flow result and the `entra-account-hand-off` Attribute hold the status,
the object ID, the group outcome, and the next step. Onboarding creates a real
account, so run it against a test tenant or a name you intend to keep, and
offboard it afterwards.

## Test

```bash
GOWORK=off go test -race ./examples/account-lifecycle/...
```

With the latest Dex development server running, the integration tests drive
the Flow on a real Worker against `internal/graphfake`: a create and a
membership add that each answer after nine seconds, so Dex dispatches a backup
attempt, with exactly one account and one membership; a lost create response
followed by two membership adds that race account replication; a name that
belongs to another account; a reinstated account that keeps its membership;
an offboarding whose first removal response is lost; an unknown account on
the unwired `notFound` branch; and an invalid request that fails before any
provider request. Every scenario that creates an account also checks that no
generated password reaches a Flow Attribute.

```bash
GOWORK=off go test -tags=integration ./examples/account-lifecycle/... -count=1 -v
```
