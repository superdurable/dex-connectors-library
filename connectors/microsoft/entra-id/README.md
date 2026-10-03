# Microsoft Entra ID Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack, through Dex Web Start Flow and real-Dex tests, against a local stand-in for Microsoft Graph and the Microsoft identity platform; no real Microsoft Entra tenant was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

The Microsoft Entra ID Connector provisions and deprovisions Microsoft Entra
user accounts and group memberships through Microsoft Graph v1.0. It exposes
these operation-specific Dex Step factories, named like the
[Google Workspace Admin Connector](../../google/workspace-admin/README.md)'s so
an onboarding Flow can swap providers:

- `entraid.NewGetUserStep` reads one account by object ID or user principal name.
- `entraid.NewListUsersStep` lists one bounded page of accounts, optionally
  filtered with an OData `$filter` or a `$search` expression.
- `entraid.NewCreateUserStep` creates one enabled account with a generated
  password that nobody receives.
- `entraid.NewDisableUserStep` and `entraid.NewEnableUserStep` block and
  restore sign-in by setting `accountEnabled`.
- `entraid.NewRevokeSignInSessionsStep` invalidates every refresh token and
  session cookie issued to the account.
- `entraid.NewAddUserToGroupStep` and `entraid.NewRemoveUserFromGroupStep`
  change one direct group membership.

Every Mutation converges when a Step repeats, so all eight operations use async
Execute durability and none declares an `uncertain` branch. The connector never
deletes or renames accounts, changes passwords after creation, assigns
licenses or directory roles, or edits groups. This release has no Triggers and
no Studio pickers.

## Microsoft Graph permissions

Both methods use the same least-privileged Microsoft Graph permissions, which
the docs list as least privileged for each call. Every one requires admin
consent; `offline_access`, which only the OAuth method requests, does not.

| Operation | Least-privileged permission | Notes |
| --- | --- | --- |
| `getUser`, `listUsers` | `User.Read.All` | `User.ReadBasic.All` cannot read `accountEnabled`. |
| `createUser` | `User.Create` and `User.Read.All` | `User.Create` creates; the read-back after a duplicate needs `User.Read.All`. |
| `disableUser`, `enableUser` | `User.EnableDisableAccount.All` and `User.Read.All` | The combination Microsoft documents for `accountEnabled`. |
| `revokeSignInSessions` | `User.RevokeSessions.All` | |
| `addUserToGroup`, `removeUserFromGroup` | `GroupMember.ReadWrite.All` | Also reads the group and its members to confirm an outcome. |

The connector never requests `User.ReadWrite.All`, `Directory.ReadWrite.All`,
or `Group.ReadWrite.All`. With app-only access you may grant only the rows your
Flows use; the OAuth method requests all five, and the refresh driver requires
all five in the returned `scope` (in short form or as
`https://graph.microsoft.com/...`) before it stores a token, but never
`offline_access`.

Microsoft treats `accountEnabled` as a sensitive property. Disabling,
enabling, or revoking sessions for an account that holds an administrator role
needs a higher role: a Privileged Authentication Administrator for delegated
calls, and the same role assigned to the app for app-only calls. Groups that
are role-assignable also need `RoleManagement.ReadWrite.Directory` and a
Privileged Role Administrator, which this connector does not request.

## Authorization

- `entra-app-only` is the recommended default. An app registration in your
  tenant, which may be single-tenant, holds the application permissions above,
  and the connection stores its Directory (tenant) ID, Application (client)
  ID, and client secret. The driver validates the tenant (a GUID or a verified
  domain; `common`, `organizations`, and `consumers` are rejected), then calls
  `oauthtoken.TokenEndpoint.ExchangeClientCredentials` against
  `https://login.microsoftonline.com/<tenant>/oauth2/v2.0/token` with scope
  `https://graph.microsoft.com/.default`, and stores the one-hour token with
  its expiry. The response carries no refresh token, so the driver repeats
  the exchange before the token expires.
- `microsoft-oauth` is interactive consent by one administrator, whose
  directory roles bound every call; User Administrator covers accounts without
  administrator roles. Manifest OAuth endpoints are static, so Dex signs in
  through `https://login.microsoftonline.com/organizations/oauth2/v2.0/...`.
  That needs a **multitenant** app registration ("Accounts in any
  organizational directory"); a single-tenant app fails with AADSTS50194.
  Personal Microsoft accounts are out of scope. Scopes are declared in short
  form with `offline_access`, which Microsoft does not echo in `scope`, so
  this method needs **Dex CLI 1.4.1 or later**, which accepts a returned
  refresh token as proof of `offline_access`. Refreshes use
  `ExchangeRefreshToken` with `ClientSecretPost` and keep Microsoft's rotated
  refresh token.

Microsoft access tokens always expire, so a token without a recorded expiry is
refreshed. `invalid_request`, `invalid_grant`, `unauthorized_client`,
`invalid_client`, `unsupported_grant_type`, `invalid_resource`,
`interaction_required`, `consent_required`, and `invalid_scope` require
reauthorization; a Microsoft 5xx is retried. A Graph 401 forces one
coordinated refresh and one resend, never a refresh loop.

The driver never owns persistence. Local development reloads and atomically
replaces the private `0600` connection file. Hosted applications receive only
an operation-scoped access token: `DecodeResolvedCredentialsJSON` rejects
client secrets and refresh tokens, and `DecodeCredentialsJSON` and
`EncodeCredentialsJSON` are the broker's trusted decode and persistence hooks.

Name the factory connection and load the same name at application startup, as
[`examples/account-lifecycle/main.go`](examples/account-lifecycle/main.go) does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := entraid.NewLocalConnection(store, accountlifecycle.ConnectionName, connectorOptions()...)
```

`WithLocalProviderURL` sends every request for `graph.microsoft.com` and
`login.microsoftonline.com` to one loopback fake for local verification; it
rejects any other host. Production Workers never set it.

## The initial password

Microsoft Graph requires a password to create a cloud account, but a password
in Step input or in a Result would be persisted in Flow state.
`CreateUserInput` therefore has no password field. Each create request
generates a 44-character password inside `Invoke` from `crypto/rand`, with at
least one lowercase letter, uppercase letter, digit, and symbol (Microsoft
Entra requires three of the four), sends it only in `passwordProfile` with
`forceChangePasswordNextSignIn: true`, and discards it. Microsoft never returns
a password, so it reaches no Result, Receipt, Attribute, Stream, or log, and a
retried create sends a fresh one.

Nobody learns the password, so the hand-off happens outside Dex: a user who
signs in through a federated identity provider never uses it; otherwise an
administrator resets the password or issues a Temporary Access Pass in the
Microsoft Entra admin center and delivers it out of band.

## Duplicate-safe account creation

Microsoft Graph has no idempotency key for `POST /users`, and it answers
`400 Bad Request` for a taken user principal name. `createUser` records a
creation key in one Exchange custom attribute,
`onPremisesExtensionAttributes.<creationKeyAttribute>`, as
`dex-creation-key:<key>`. Microsoft documents that cloud-only accounts accept
these attributes at creation. The key is `provisioningKey` when the
application sets one, such as an HR hire ID, and otherwise the Step's Call ID,
which every attempt of one Step execution shares. Choose a
`creationKeyAttribute` your tenant does not already use.

Microsoft asks callers not to depend on error message text, and a 400 has
several causes, so every 400 reads the name back with `GET /users/{upn}`:

- an account carrying the key is this Step's account, so a duplicate dispatch,
  a lost response, or a retry after a 5xx converges on it as `created` with
  `wasAlreadyCreated: true`;
- an account without the key selects `alreadyExists` and returns that account
  without changing it;
- no readable account is retried for one minute from the Step's first attempt,
  because Microsoft documents replication delay after a create; after that, a
  400 whose `error.details` code is `ObjectConflict` selects `alreadyExists`
  (the name belongs to a deleted account or another object), and any other
  400 selects `providerRejected`.

`createUser`, `disableUser`, `enableUser`, and `addUserToGroup` use eight
attempts over at most five minutes, so the last attempt starts after that
window. Set `provisioningKey` to deduplicate across Step executions or Flow
runs; without it, a new Step execution that finds the earlier account selects
`alreadyExists`. The attribute stays on the account and is visible to
administrators. Accounts in a federated domain need an `onPremisesImmutableId`
that this connector does not send, so Microsoft rejects them.

## Account state and sessions

`disableUser` and `enableUser` send `PATCH /users/{key}` with only
`{"accountEnabled": false}` or `true`, which Microsoft answers with 204, then
read the account back. A read-back that still shows the old value is retried
within the one-minute window and then selects `providerRejected` with the
returned account; so does an account synchronized from on-premises Active
Directory, which Graph cannot change. Disabling keeps the account's data.

`revokeSignInSessions` calls `POST /users/{key}/revokeSignInSessions`, which
resets `signInSessionsValidFromDateTime` to now. Microsoft documents a 2xx
answer, a delay of a few minutes before tokens stop working, and no effect on
external users' home-tenant sessions. Repeating it only moves the cutoff
forward, so a lost response is retried; `{"value": false}` selects
`providerRejected`.

## Group membership

Groups and accounts are addressed by object ID, because Graph's membership
references require it. `addUserToGroup` sends `POST /groups/{id}/members/$ref`
with `@odata.id` set to `https://graph.microsoft.com/v1.0/directoryObjects/{userId}`.
Microsoft answers 400 for an existing member, an unsupported member, and a
group that has not replicated, so a 400 checks the group's direct members
with `$filter=id eq '{userId}'`, `$count=true`, and `ConsistencyLevel:
eventual`: a listed member is reported as `added` with `wasAlreadyMember:
true`; otherwise the check is retried within the one-minute window, because
that index can lag, and then selects `providerRejected`. A 404 reads the group:
a missing group selects `notFound`, and an existing one means the account has
not replicated yet, which is retried within the window and then selects
`notFound`. Distribution, mail-enabled security, and dynamic groups cannot be
managed through Graph; Microsoft answers 403 or 400, which selects
`providerRejected`.

`removeUserFromGroup` sends `DELETE /groups/{id}/members/{userId}/$ref`. The
path always ends in `/$ref`: without it, an app that can manage users would
delete the account itself. A 404 reads the group: an existing group means the
membership is already gone (`removed` with `wasAlreadyRemoved: true`), and a
missing group selects `notFound`. Membership through a nested group is not
changed.

## Listing and reading accounts

`getUser` and `listUsers` always send `$select`, so every account is the
bounded `User` record: object ID, user principal name, names, mail and alias,
`accountEnabled`, user type, job title, department, usage location,
on-premises sync state, creation time, and the sign-in session cutoff, exactly
as Microsoft reports them. Phones, addresses, authentication methods, and
extension attributes are never returned. A name that starts with `$` uses
Microsoft's `/users('...')` key syntax, and a guest's `#EXT#` name is
percent-encoded.

`listUsers` requests `$top` accounts, 1 to 999, where zero uses the
connection's `listUsersPageSize`. `filter` is passed as `$filter`; `search`
(a quoted expression such as `"displayName:Ada"`) and `usesAdvancedQuery`
send `ConsistencyLevel: eventual`, the latter with `$count=true`, as
Microsoft requires for advanced queries. Those results come from an index
that can lag, so use `getUser` to confirm a just-created account.
`nextPageToken` is Microsoft's complete `@odata.nextLink`; the connector
follows one only when its scheme, host, and path are
`https://graph.microsoft.com/v1.0/users`, and Microsoft says not to take it
apart.

## Branches

| Outcome | Branch or Retry |
| --- | --- |
| Invalid input, configuration, or credential | `defect`, before any request |
| 404 | `notFound` where the operation declares it, after the checks above |
| 400 | read-back or membership check for `createUser` and `addUserToGroup`, otherwise `providerRejected` |
| 401 after one forced refresh, 403, and other 4xx | `providerRejected` |
| Malformed or oversized response | `invalidResponse`, or Retry when a write may have been applied |
| 429, 408, 5xx except 501, 409 `Directory_ConcurrencyViolation`, transport failure | Retry, honoring `Retry-After` up to one hour |

Failures carry a safe message, the failure kind, and only a documented Graph
error code such as `Authorization_RequestDenied`, never `error.message`,
account data, or credentials. Error bodies are read with their own 64 KiB
bound, and redirects are never followed with the credential. Only each
operation's happy-path branch is required; every other branch is optional and
fails the Flow when unwired.

## Why no pickers or Triggers

A group picker would fit Graph's Studio command shape, but the default
app-only method stores only a token the Worker obtains and renews; Dex Web
cannot run a client credentials exchange, so the picker would fail whenever
that token is missing or expired. The team group also usually varies per hire
and comes from the HR record, and onboarding and offboarding are separate
Steps that would each need their own pick. Group and account IDs therefore
come from Start Flow input.

Triggers are deferred. A user-change Trigger would need either Graph change
notifications, which require the `validationToken` handshake that
`webhooktrigger` does not have yet plus subscription renewal before
expiry, or `users/delta` polling, which needs a durable per-binding delta
cursor that `sdkgo` does not provide.

## Example

[`examples/account-lifecycle`](examples/account-lifecycle) is a runnable Dex
Web **Start Flow** example. Onboarding creates the hire's account, or finds
the one an earlier run created for the same hire ID, and adds it to a team
group; a name that belongs to someone else is handed to IT without creating
anything; reinstating enables a returning person's account instead of
creating a second one; and offboarding finds the account, disables it,
revokes its sign-in sessions, and removes the membership.

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

`internal/graphfake` is the credential-safe stand-in both suites use. It
serves the Graph endpoints above and the token endpoint, returns the
documented response shapes (a 201 without `accountEnabled`, 204 for writes,
`{"value": true}` for a revocation), and puts a sentinel in every error
message so tests prove it never reaches a Failure.

Not verified against a real tenant: whether `User.Create` permits setting an
extension attribute at creation; the exact 400 Microsoft returns for a taken
name, an existing member, and a non-member removal (404 is assumed), including
the `ObjectConflict` detail code; `$filter=id eq` on group members; the shape
of the `scope` string in a delegated token response; Dex Web consent through
the organizations endpoint; app-only role requirements for sensitive actions
on ordinary accounts; and admin center paths in the setup guide.
