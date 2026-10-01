# Zoom Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Zoom; no live Zoom account was used. See
> [verification status](../../docs/verification-status.md) for what is and
> is not verified.

This module schedules and follows the authorized user's Zoom meetings through
the Zoom Meetings API with user-level OAuth:

| Operation | Kind | Happy branch | Other branches |
| --- | --- | --- | --- |
| `listMeetings` | Query | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `getMeeting` | Query | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `createMeeting` | Mutation, sync | `created` | `providerRejected`, `uncertain`, `defect` |
| `updateMeeting` | Mutation | `updated` | `notFound`, `providerRejected`, `defect` |
| `listPastMeetingParticipants` | Query | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

Every operation acts for the user who authorized the connection: Zoom requires
the `me` user for user-level OAuth apps. Only a safe meeting view crosses the
connector boundary. Results, receipts, failures, and logs never contain the
host's `start_url`, which signs anyone in as the host, any meeting passcode,
alternative hosts, Zoom's error message text, or a credential. The `joinUrl` is
returned because a scheduling Flow sends it to the guest; it can embed the
passcode, so share it only with invitees. A failure keeps only Zoom's numeric
error code, such as `3161` for a user who cannot host, in its message and in
the Receipt's `zoomErrorCode` metadata.

## Time

Every start is an RFC 3339 instant with an explicit `Z` or `±hh:mm` offset,
such as `2026-10-08T09:00:00-07:00`, plus an IANA time zone such as
`America/Los_Angeles`. A start without an offset, a time zone abbreviation such
as `PST`, or a fractional second selects `defect` without a request. The
connector sends the instant to Zoom in its documented GMT form,
`2026-10-08T16:00:00Z`, and sends the time zone separately, so the instant
never depends on Zoom's time zone table; the zone only decides how Zoom
displays the meeting. Zoom silently replaces a past `start_time` with the
current time on create and ignores it on update, so both operations reject a
start that is not in the future. Results report `startTime` in UTC and
`timeZone` separately; compare instants, not text.

## Zoom setup

The connection uses a Zoom **General app** that is **user-managed**:

1. Sign in at [marketplace.zoom.us](https://marketplace.zoom.us/) with an
   account that has developer permissions, choose **Developer**, then
   **Develop > Build an app > General app > Create**.
2. On **Basic Information**, choose **User-managed**, so the app reaches only
   the meetings of the user who authorizes it.
3. Open Dex Web at `http://127.0.0.1:<port>`, not `localhost`, because Zoom
   rejects `localhost` redirects. Under **Basic Information > OAuth
   Information**, set **OAuth redirect URL** to the Redirect URI Dex Web shows
   and add the same URI to **OAuth allow lists**.
4. On **Scopes**, choose **Add Scopes > Meeting** and add these granular
   scopes. Dex Web requires every one in Zoom's granted `scope` string, so an
   app limited to the classic `meeting:read` and `meeting:write` scopes fails
   authorization.

   | Scope | Used by |
   | --- | --- |
   | `meeting:read:list_meetings` | `listMeetings` |
   | `meeting:read:meeting` | `getMeeting` |
   | `meeting:write:meeting` | `createMeeting` |
   | `meeting:update:meeting` | `updateMeeting` |
   | `meeting:read:list_past_participants` | `listPastMeetingParticipants` |

5. Copy the development **Client ID** and **Client Secret** from **Basic
   Information > App Credentials** into Dex Web and choose **Authorize** as the
   user who hosts the meetings. Until the app is published, only members of the
   developer's Zoom account can authorize it.

Dex Web stores the access and refresh tokens. Credentials are reread before
every provider call, so reauthorizing in Dex Web takes effect without a
restart. The endpoint and response limit are startup configuration.

## Token refresh

Zoom access tokens last one hour. Before a call, the connector refreshes a
token that expires within five minutes through `sdkgo/oauthtoken`, posting the
`refresh_token` grant to `https://zoom.us/oauth/token` with the client as HTTP
Basic credentials, as Zoom documents. Zoom returns a new refresh token on every
refresh and tells clients to use the latest one; the local credential provider
writes the replacement atomically before the next call. A 401 with Zoom code
`124` forces one locked refresh and one more request; a second 401 selects
`providerRejected`.

Refresh tokens expire after 90 days. A refresh that Zoom answers below HTTP 500
with `invalid_request`, `invalid_client`, `invalid_grant`, or
`unauthorized_client`, or that returns a scope string missing a required
scope, marks the connection `reauthorization_required`, and the operation
selects `defect` without a request. Zoom documents no refresh error codes of
its own, so these are the RFC 6749 section 5.2 codes. A 5xx or another failed
refresh is always retried, and the operation returns Retry, which is safe even
for `createMeeting` because nothing was sent.

Server-to-Server OAuth apps use Zoom's `account_credentials` grant, which
`sdkgo/oauthtoken` does not implement, so this release supports only user-level
OAuth.

## Local configuration

Dex Web writes this record for the connection name the application uses:

```json
{
  "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
  "connections": [{
    "connectorId": "zoom",
    "modulePath": "github.com/superdurable/dex-connectors-library/connectors/zoom",
    "moduleVersion": "v0.1.0",
    "provider": "zoom",
    "connectionName": "zoom-scheduler",
    "configuration": {},
    "credentials": {"oauth_client_id": "...", "oauth_client_secret": "...", "access_token": "...", "refresh_token": "..."},
    "credentialExpiresAt": "2026-10-08T17:00:00Z"
  }]
}
```

Load it with `localconfig.LoadFromEnvironment` and
`zoom.NewLocalConnection(store, "zoom-scheduler")`, as
[`examples/booked-meeting/main.go`](examples/booked-meeting/main.go) does.

In a hosted deployment, pass `zoom.DecodeResolvedCredentialsJSON` to
`hostedconfig.NewCredentialProviderFromEnvironment`. It accepts only
`{"access_token": "..."}`, because refresh material stays in the broker.

## Operations

### listMeetings

`ListMeetingsInput.Type` is `upcoming` (the default), `scheduled`, `live`, or
`previous_meetings`; Zoom lists scheduled meetings that have not expired, never
instant meetings, and returns at most six months for `upcoming` and
`previous_meetings`. `PageSize` is 1 to 300 and defaults to Zoom's 30.
`NextPageToken` requests the next page; Zoom expires it after 15 minutes. Zoom
truncates a listed agenda to 250 characters.

### getMeeting

Reads one meeting by its numeric ID, including its settings. A deleted or
unknown meeting (`404`, code `3001`) selects `notFound`.

### createMeeting

`CreateMeetingInput` takes a topic of 1 to 200 characters, the start and time
zone described above, a duration of 1 to 1440 minutes, an optional agenda of
at most 2000 characters, and optional settings: host and participant video,
join before host, mute upon entry, waiting room, and automatic recording
(`none`, `local`, or `cloud`). A blank setting keeps the user's default.

Zoom's create API has no idempotency key: a repeated request makes a second
meeting. The connector therefore sends a create only when no earlier attempt
could have sent it:

| Outcome | Result |
| --- | --- |
| 201 with a valid meeting | `created`, with the meeting ID and join URL |
| 400, 401, 403, 404, or another 4xx except 408 and 429 | `providerRejected`, with Zoom's error code |
| 429 whose `Retry-After` is at most one minute, or none | Retry after the delay |
| 429 whose `Retry-After` is later, as for Zoom's daily limit | `providerRejected` with `RATE_LIMIT` |
| DNS, connect, or TLS failure before any connection opened | Retry |
| Timeout, dropped connection, 3xx, 408, or 5xx after dispatch | `uncertain` |
| 2xx whose body is oversized, unreadable, unusable, or reflects the credential | `uncertain` |
| An earlier attempt of the same Step execution sent the request | `uncertain`, without a request |

The manifest makes `createMeeting` sync. With async durability, Dex runs the
Step in a local phase of about seven seconds and then starts a fallback
attempt, so a slow create is sent twice. In a real Dex run with a fake that
answered after nine seconds, async durability left two meetings and sync left
one; `TestSlowCreateIsSentOnceWithRealDex` guards this. Do not override it to
async: the dispatch checkpoint below does not survive the local phase.

Before it sends a create, the operation records a Dex heartbeat checkpoint. A
later attempt of the same Step execution, such as the retry Dex starts after
losing the Worker mid-request, finds the checkpoint and selects `uncertain`
instead of sending again. A 429 or a provably undispatched request clears it,
so those retries may create. `TestWorkerLostDuringCreateIsReconciledWithoutCreatingAgainWithRealDex`
kills a Worker process one second into its request and proves the replacement
creates nothing; with the checkpoint removed, the same test records two
meetings. Dex sends the heartbeat without waiting for it to persist, so a
Worker lost in the instant between recording it and sending the request can
still cause one more create. The connector bounds every request by 20 seconds,
below the 30-second Execute timeout, so a hung create selects `uncertain`
before Dex would retry the Step.

On `uncertain` and `providerRejected`, `Value` has no ID and echoes the
requested topic, start, duration, time zone, and agenda for reconciliation.
[`examples/booked-meeting`](examples/booked-meeting) reconciles an unknown
create with `listMeetings` instead of creating again:

```go
		dex.DefineStep(zoom.NewCreateMeetingStep(zoom.CreateMeetingStepConfig[Input]{
			StepType: createBookedMeetingStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zoom", GroupLabel: "Zoom",
				Explanation: "Create the meeting once; an unknown outcome is reconciled, never created again automatically.",
			},
			Connection: flow.connection, MapToOperationInput: MapToCreateMeetingInput,
			Created:          sdkgo.GoTo(recordScheduledMeeting{}),
			ProviderRejected: sdkgo.GoTo(recordRejectedMeeting{}),
			Uncertain:        sdkgo.GoTo(recordUncertainMeeting{}),
		})),
```

Zoom limits each host to 100 meeting create and update requests per UTC day.

### updateMeeting

`UpdateMeetingInput` patches the topic, agenda, start and time zone (always
together), duration, or settings of one meeting; a patch with no change
selects `defect`. Every set field replaces Zoom's value, so repeating a patch
leaves the same meeting, and the operation keeps the async default: transport
failures, 408, 429 with a short `Retry-After`, and 5xx are retried. In a real
Dex run with a fake that answered after nine seconds, Dex sent the patch twice
with identical bodies and the meeting held the requested slot once; see
`TestSlowRescheduleIsSafeToRepeatWithRealDex`. Zoom answers `204` with no
body, so read the meeting back with `getMeeting` to observe the stored values.
A deleted meeting selects `notFound`. Zoom limits one meeting to 100 updates per
day.

### listPastMeetingParticipants

Lists one page, up to 300, of the joins to the latest ended instance of a
meeting. Zoom lists a person once per join, leaves the email blank for most
people outside the host's account, and by default omits meetings with only
one participant. A meeting without an ended instance selects `notFound`. A free
account (code `200`) and a meeting older than Zoom keeps (code `12702`) select
`providerRejected`. The endpoint needs a Pro plan or higher.

## Not in this release

- **Triggers.** Zoom event subscriptions require a URL validation handshake
  before Zoom delivers events, which `sdkgo/webhooktrigger` does not support
  yet. Until then, the example ends its wait with a Timer or an Action.
- **Pickers.** No operation or example field takes a provider resource the user
  would pick: the host is always the authorizing user, and meeting IDs come
  from Flow data. The manifest declares no Studio bundle.
- **Server-to-Server OAuth**, as described under Token refresh.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the pinned Dex development server running, the example's real Dex
integration tests cover Worker, retry, RPC, Channel, Timer, persistence, and
recovery behavior:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```

From the repository root, check the manifest and generated code:

```bash
go run ./cmd/connectorctl validate connectors/zoom/connector.yaml
go run ./cmd/connectorctl generate --check connectors/zoom/connector.yaml
```

The deterministic provider fakes cover every branch above, the request shapes,
the refresh exchange and rotation through a local connections file, credential
reflection, redirects, oversized and malformed responses, and the absence of
start URLs, passcodes, and Zoom message text in results.

No live Zoom account was used. The following behavior is unverified:

- whether Zoom accepts Dex Web's authorization-code exchange, which sends
  `client_id` and `client_secret` in the form body, while Zoom documents only
  HTTP Basic client authentication for confidential apps;
- whether Zoom accepts Dex Web's plain-HTTP `127.0.0.1` Redirect URI for a
  confidential app that sends PKCE, since Zoom documents loopback redirects for
  PKCE and native clients and HTTPS otherwise;
- how Zoom treats the `scope` parameter Dex Web adds to the authorization URL,
  and the exact granular scope string Zoom returns;
- Zoom's refresh error codes and whether a used refresh token stops working;
- whether Zoom sends `Retry-After` on per-second and daily 429 responses;
- real meeting, listing, and participant response shapes, and how Zoom handles
  a time zone outside its supported list;
- the Marketplace page paths in the setup guidance.
