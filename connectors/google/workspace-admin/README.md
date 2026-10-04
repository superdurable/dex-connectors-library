# Google Workspace Admin Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for the Google Admin SDK; no real Google Workspace tenant was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

The Google Workspace Admin Connector provisions and deprovisions Google
Workspace user accounts and group memberships through the Admin SDK Directory
API. It exposes these operation-specific Dex Step factories:

- `workspaceadmin.NewGetUserStep` reads one account by primary address, alias,
  or unique user ID.
- `workspaceadmin.NewListUsersStep` lists one bounded page of accounts in the
  administrator's customer account or one domain, optionally filtered with
  Google's user search syntax.
- `workspaceadmin.NewCreateUserStep` creates one account with a generated
  password that nobody receives.
- `workspaceadmin.NewSuspendUserStep` and `workspaceadmin.NewUnsuspendUserStep`
  block and restore sign-in.
- `workspaceadmin.NewAddUserToGroupStep` and
  `workspaceadmin.NewRemoveUserFromGroupStep` change one direct group
  membership.

Every Mutation converges when a Step repeats, so all seven operations use async
Execute durability and none declares an `uncertain` branch. The connector never
deletes or renames accounts, changes passwords after creation, grants
administrator roles, or edits group settings. This release has no Triggers and
no Studio pickers.

## Scopes

Both authorization methods request exactly these scopes, each by its canonical
URI:

| Scope | Why |
| --- | --- |
| `https://www.googleapis.com/auth/admin.directory.user` | `users.get`, `users.list`, `users.insert`, and `users.update`. Google offers no narrower scope that can create or suspend an account; `admin.directory.user.readonly` cannot write, and `admin.directory.user.security` covers tokens and app passwords, not suspension. |
| `https://www.googleapis.com/auth/admin.directory.group.member` | `members.insert`, `members.get`, `members.list`, and `members.delete`. The broader `admin.directory.group` scope would also create and delete groups and change their aliases. |

Neither scope lists organizational units or groups, so the connector ships no
Studio picker: an organizational-unit picker would need
`admin.directory.orgunit.readonly` and a group picker
`admin.directory.group.readonly`. Organizational unit and group are per-account
Step input instead, which usually comes from an HR record. A token that reports
fewer scopes than both requires reauthorization.

## Authorization

The scopes grant nothing by themselves: every call runs with the privileges of
one Workspace administrator. Use an administrator holding the prebuilt **User
Management Admin** and **Groups Admin** roles. A User Management Admin cannot
change administrator accounts, so only Flows that must suspend or create
administrators need a super administrator.

- `workspace-domain-delegation` is the recommended default. A super
  administrator authorizes the service account's client ID for the two scopes
  at **Security > Access and data control > API controls > Manage Domain Wide
  Delegation**, and `delegated_user` names the administrator the service
  account acts as. The driver signs an RS256 assertion for that administrator
  with `oauthtoken.SignJWTBearerAssertion` and mints a one-hour access token
  without interactive consent. Google documents that a delegation change can
  take up to 24 hours.
- `google-oauth` is interactive consent by the administrator. Choose the
  Internal audience so only your organization's accounts can authorize; an
  External app in Testing needs listed test users and its refresh tokens can
  expire after seven days. Dex requests offline access and refreshes with
  `oauthtoken.TokenEndpoint.ExchangeRefreshToken` and `ClientSecretPost`.

Google access tokens always expire, so a token without a recorded expiry is
refreshed (`RefreshWhenExpiryMissing`). `invalid_grant`, `invalid_client`,
`unauthorized_client`, missing refresh material, an invalid service-account key
or delegated administrator, and a token without both scopes require
reauthorization; a Google 5xx is retried. If Google omits a new refresh token,
the prior one is kept. After a 401 the connector asks once for a refresh,
which the project connection performs only when the stored expiry has passed,
and then resends once; otherwise the 401 selects `providerRejected`. There is
never a refresh loop.

The driver never owns persistence. The project connection that
`NewProjectConnection` opens admits one refresh per credential generation
across application replicas and stores the complete replacement before the call
uses it. Refresh tokens, client secrets, and service-account keys stay in
encrypted project storage and never enter a Flow.

Load the project configuration once at application startup and open the
connection by the name its operations use, as
[`examples/account-lifecycle/main.go`](examples/account-lifecycle/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := workspaceadmin.NewProjectConnection(project, accountlifecycle.ConnectionName)
```

`projectconfig.LoadFromEnvironment` reads the `DEX_PROJECT_*` configuration
that Dex Web or Superverse Studio writes; see
[`sdkgo/projectconfig`](../../../sdkgo/projectconfig/README.md#application-loading).
Set the same `ConnectionName` beside the typed `Connection` in each operation:
a Step whose `ConnectionName` is empty or differs from its connection's name
panics at construction.

## The initial password

Google requires a password to create an account, but a password in Step input
would be persisted in Flow state, and one in a Result would be persisted too.
`CreateUserInput` therefore has no password field. Each insert generates 256
random bits as a 43-character URL-safe password inside `Invoke`, sends it only in
that request body, sets `changePasswordAtNextLogin`, and discards it. Google
never returns a password, so it reaches no Result, Receipt, Attribute, Stream,
or log, and a retried insert sends a fresh one.

Nobody learns the password, so the hand-off happens outside Dex:

- an account that signs in through a third-party SSO provider never uses it;
- otherwise an administrator opens **Admin console > Directory > Users**,
  chooses **Reset password**, and delivers the new password out of band;
- `recoveryEmail` and `recoveryPhone` let Google's own account recovery reach
  the person when your domain allows user account recovery.

## Duplicate-safe account creation

`createUser` records a creation key on the account as a custom external ID,
`{"type": "custom", "customType": "dexIdempotencyKey", "value": KEY}`. The key
is `provisioningKey` when the application sets one, such as an HR hire ID, and
otherwise the Step's Call ID, which every attempt of one Step execution shares.

Google answers 409 when an address is taken. The connector then reads the
address back with `users.get`:

- an account carrying the key is this Step's account, so a duplicate dispatch,
  a lost response, or a retry after a 5xx converges on it as `created` with
  `wasAlreadyCreated: true`;
- an account without the key, including one that holds the address as an
  alias, selects `alreadyExists` and returns that account, without changing it;
- no readable account is retried for one minute from the Step's first attempt,
  because Google documents a propagation delay after creation, and then
  selects `alreadyExists`: the address belongs to a group or to an account
  outside the directory, such as the unmanaged account Google's `users.insert`
  reference names as a 409 cause.

An unusable success response is retried for the same reason. `createUser` and
`addUserToGroup` use eight attempts over at most five minutes, so the last
attempt starts after that one-minute window. Set `provisioningKey` to
deduplicate across Step executions or Flow runs; without it, a new Step
execution that finds the earlier account selects `alreadyExists`. The external
ID stays on the account and is visible to administrators through the API.

Google bills a license for each created account on a flexible plan, and limits
creation to 10 accounts per domain per second; a creation 503 is retried.

## Suspension

`suspendUser` and `unsuspendUser` send `users.update` with only
`{"suspended": true}` or `{"suspended": false}`. Google documents patch
semantics for `users.update`, so no other field changes and a repeated attempt
applies nothing new. An account Google returns in the other state, such as one
Google suspended for abuse, selects `providerRejected` with the returned
account. A missing account selects `notFound`. Suspension keeps the account's
data.

## Group membership

`addUserToGroup` inserts a direct membership with role `MEMBER`, `MANAGER`, or
`OWNER`, defaulting to `MEMBER`. A 409 reads the membership back and returns it
as `added` with `wasAlreadyMember: true` and its existing role, which the
connector never changes. A missing group or account selects `notFound`. Google
documents that a call made right after creating an account can fail with
"User creation is not complete"; that error, whatever its status, is retried.

`removeUserFromGroup` deletes a direct membership by address or unique user ID.
Google answers 404 both for a missing group and for an account that is not a
member, so a 404 is followed by a one-member `members.list` of the group: an
existing group means the membership is already gone (`removed` with
`wasAlreadyRemoved: true`), and a missing group selects `notFound`. Membership
through a nested group is not changed.

## Listing accounts

`listUsers` sends `customer=my_customer` unless the input sets `domain` or a
reseller's `customer` ID, sorts by `email` unless `orderBy` is `givenName` or
`familyName`, and requests `pageSize` accounts, 1 to 500, where zero uses the
connection's `listUsersPageSize`. `query` passes Google's user search syntax,
such as `orgUnitPath='/Sales' isSuspended=false`. Google documents that new
data can take up to 36 hours to reach search results, so use `getUser`, not a
search, to confirm a just-created account.

Every account is returned as the bounded `User` record: ID, primary address,
name, organizational unit, suspension and archive state, administrator and
2-Step Verification flags, aliases, customer ID, and creation and last sign-in
times exactly as Google reports them. Recovery details, phones, addresses,
external IDs, and custom schema fields are never returned.

## Branches

| Outcome | Branch or Retry |
| --- | --- |
| Invalid input, configuration, or credential | `defect`, before any request |
| 404 | `notFound` where the operation declares it, otherwise `providerRejected` |
| 409 | read-back convergence for `createUser` and `addUserToGroup`, otherwise `providerRejected` |
| Other 4xx, including a 403 without a rate-limit reason | `providerRejected` |
| Malformed or oversized response | `invalidResponse`, or Retry when a write may have been applied |
| 429; 403 `userRateLimitExceeded`, `rateLimitExceeded`, or `quotaExceeded`; 408; 5xx; transport failure | Retry, honoring `Retry-After` up to one hour |

Failures carry a safe message, the failure kind, and only a documented Google
reason code, never provider message text, account data, or credentials. Error
bodies are read with their own 64 KiB bound, and redirects are never followed
with the credential. Only each operation's happy-path branch is required; every
other branch is optional and fails the Flow when unwired.

## Example

[`examples/account-lifecycle`](examples/account-lifecycle) is a runnable Dex
Web **Start Flow** example. Onboarding creates the account, adds it to a team
group, reads it back, and records the hand-off; an address that belongs to
someone else is handed to IT without creating anything; reinstating restores a
returning person's account instead of creating a second one; and offboarding
suspends the account and removes the membership.

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
