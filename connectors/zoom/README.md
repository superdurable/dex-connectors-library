# Zoom Connector

## TypeScript package

Install `@superdurable/connector-zoom@0.1.0` with Connector SDK 0.6.0 to use all five meeting operations from TypeScript. The [package README](typescript/README.md) documents the generated OAuth connection binding, operation-specific Step factories, and the runnable meeting-list Flow. The TypeScript conformance suite replays Go-recorded provider requests and decisions; its Dex integration check runs the example with a generated stand-in. Live Zoom OAuth authorization and provider behavior remain unverified.

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/zoom/go@v0.25.0`. It requires Connector SDK `sdkgo` v0.38.0, which adds the poll Trigger kit.

Moved from `github.com/superdurable/dex-connectors-library/connectors/zoom`.

> **Verification status: Dex-integrated, not live.** The example paths, including the 503 retry and mock-backed scenarios, ran on a real Dex stack in CI against a local stand-in for Zoom. No live Zoom account was used. See
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

The Go `meetingChanged` Trigger polls the same meeting list and starts a Flow
for each meeting scheduled, updated, or removed; see
[meetingChanged Trigger](#meetingchanged-trigger).

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
start that is not in the future before its first send. A later create attempt
checks the dispatch fence first, so a saved meeting ID is read back even after
the requested start has passed. Results report `startTime` in UTC and
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
token that is missing, has no recorded expiry, or expires within five minutes.
The refresh goes through `sdkgo/oauthtoken`, posting the `refresh_token` grant
to `https://zoom.us/oauth/token` with the client as HTTP Basic credentials, as
Zoom documents. Zoom returns a new refresh token on every refresh and tells
clients to use the latest one; project storage writes the replacement
atomically before the next call. After a 401 the connector asks once for a
refresh, which project storage performs only when the recorded expiry has
passed. If it cannot refresh, the 401 selects `providerRejected`.

For `createMeeting`, a 401 triggers refresh but the fenced operation returns
Retry after recording that the request was rejected; its next Dex attempt
sends with the refreshed credential. Other operations may send again within
the same attempt after a successful refresh.

Refresh tokens expire after 90 days. A refresh that Zoom answers below HTTP 500
with `invalid_request`, `invalid_client`, `invalid_grant`, or
`unauthorized_client`, or that returns a scope string missing a required
scope, marks the connection `reauthorization_required`, and the operation
selects `defect` without a request. Zoom documents no refresh error codes of
its own, so these are the RFC 6749 section 5.2 codes. Before a `createMeeting`
send, reauthorization records `notSent` and selects `defect`; if a prior create
has an accepted ID, reauthorization prevents read-back and selects `uncertain`.
A transient refresh failure before a provider request returns Retry. After a
create request receives 401, a failed refresh selects `providerRejected` with
reauthorization or temporary-unavailability guidance and no second send.

Server-to-Server OAuth apps use Zoom's `account_credentials` grant, which
`sdkgo/oauthtoken` does not implement, so this release supports only user-level
OAuth.

## Project configuration

Dex Web or Superverse Studio saves the connection's settings and credentials in
the project configuration. The application loads that configuration once and
opens the connection by the name it declares, as
[`examples/booked-meeting/main.go`](go/examples/booked-meeting/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := zoom.NewProjectConnection(project, bookedmeeting.ConnectionName)
if err != nil {
	return err
}
```

`LoadFromEnvironment` reads the `DEX_PROJECT_*` environment described in
[project configuration](../../sdkgo/projectconfig/README.md). Credentials stay
in project storage and are read for every provider call.

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

Zoom documents no create idempotency key or lookup by connector Call ID. The
connector uses the Connector SDK [single-dispatch fence](../../sdkgo/README.md#single-dispatch-fence)
to authorize at most one create per attempt and permit another only after a
definite rejection or positive evidence that no request was sent. This
behavior has not been verified against a live Zoom account.

| Outcome | Result |
| --- | --- |
| 201 with a valid meeting | `created`, with the meeting ID and join URL |
| 400, 403, 404, or another definite 4xx except 401, 408, 409, and 429 | `providerRejected`, with Zoom's error code |
| 401 | Force one credential refresh; on success checkpoint permission to retry, otherwise `providerRejected` |
| 429 with Zoom's documented daily-limit message | `providerRejected` with a connector-owned rate-limit message; no resend checkpoint |
| Other 429, including per-second and unrecognized messages, or 503 | Checkpoint permission to retry after Zoom's delay when supplied; retry exhaustion fails the Flow |
| DNS, connect, or TLS failure before any connection opened | Retry |
| Timeout, dropped connection, 3xx, 408, 409 without a lookup key, or another 5xx after dispatch | `uncertain` |
| 2xx whose body is oversized, unreadable, unusable, or reflects the credential | `uncertain` |
| Later attempt with an accepted meeting ID checkpoint | Read the meeting back; transient read failure retries without another create |
| Later attempt with an unconfirmable checkpoint | `uncertain`, without another create |

The manifest makes `createMeeting` synchronous and declares its dispatch
fence. Do not override this Step to async: local-phase heartbeats are not
visible to later attempts. The fence reads the prior checkpoint before
credentials, claims the dispatch before sending, and limits the request to
7/12 of the smaller Step or Execute budget. A lost or stale checkpoint never
permits a resend. The accepted meeting ID, when available, lets a later
attempt use `getMeeting` to confirm it. Legacy create checkpoints fail closed
on deployment and may need the example's reconciliation path. The existing
Worker-loss test and a new 503-then-success real-Dex test cover these paths in
CI.

[Zoom's rate-limit guidance](https://developers.zoom.us/docs/api/rate-limits/)
distinguishes daily and per-second 429 responses by message. The connector
matches the documented daily-limit wording without exposing Zoom's text in
failures or receipts. It retries any 429 whose message is absent, malformed, or
does not match that wording.
[Zoom's error guidance](https://developers.zoom.us/docs/api/errors/) describes
503 as temporary unavailability. Zoom's behavior for ambiguous transport
loss and other 5xx responses remains unverified against a live account, so
the connector fails closed on those outcomes.

On `uncertain` and `providerRejected`, `Value` has no ID and echoes the
requested topic, start, duration, time zone, and agenda for reconciliation.
[`examples/booked-meeting`](go/examples/booked-meeting) reconciles an unknown
create with `listMeetings` instead of creating again:

```go
		dex.DefineStep(zoom.NewCreateMeetingStep(zoom.CreateMeetingStepConfig[Input]{
			StepType: createBookedMeetingStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zoom", GroupLabel: "Zoom",
				Explanation: "Create the meeting once; an unknown outcome is reconciled, never created again automatically.",
			},
			Connection: flow.connection, MapToOperationInput: MapToCreateMeetingInput,
			Created: sdkgo.GoTo(recordScheduledMeeting{}),
			// An uncertain create continues through meeting read-back and adoption.
			Uncertain: sdkgo.GoTo(recordUncertainMeeting{}),
		})),
```

Zoom limits each host to 100 meeting create and update requests per UTC day.
All five operations select `providerRejected` for a 429 with Zoom's documented
daily-limit message. Other 429 responses retry; if Zoom's delay outlasts the
create Step's two-minute retry budget, Dex fails the Flow on retry exhaustion.

### updateMeeting

`UpdateMeetingInput` patches the topic, agenda, start and time zone (always
together), duration, or settings of one meeting; a patch with no change
selects `defect`. Every set field replaces Zoom's value, so repeating a patch
leaves the same meeting, and the operation keeps the async default: transport
failures, 408, 409, other 429, and 5xx are retried. In a real
Dex run, a delayed patch leaves the requested slot once; see
`TestSlowRescheduleIsSafeToRepeatWithRealDex`. Zoom answers `204` with no
body, so read the meeting back with `getMeeting` to observe the stored values.
A deleted meeting selects `notFound`. Zoom limits one meeting to 100 updates per
day. A 429 with Zoom's documented daily-limit message selects
`providerRejected`; an unrecognized 429 retries.

### listPastMeetingParticipants

Lists one page, up to 300, of the joins to the latest ended instance of a
meeting. Zoom lists a person once per join, leaves the email blank for most
people outside the host's account, and by default omits meetings with only
one participant. A meeting without an ended instance selects `notFound`. A free
account (code `200`) and a meeting older than Zoom keeps (code `12702`) select
`providerRejected`. The endpoint needs a Pro plan or higher.

## meetingChanged Trigger

`meetingChanged` delivers each meeting the authorized user schedules, changes,
or that leaves Zoom's scheduled list. Zoom's List meetings API has no change
feed, sync token, or modification time, so each poll reads every page of
`GET /users/me/meetings?type=scheduled` (all unexpired previous, live, and
upcoming meetings, 300 per page) and compares it with the snapshot committed by
the last poll. The snapshot is the durable cursor: it keeps each meeting's ID,
host, topic, type, start, duration, time zone, listed agenda, and creation time,
never its UUID or join URL, and commits only after every change in it has been
recorded.

| `changeKind` | Meaning | `meeting` | `previousMeeting` |
| --- | --- | --- | --- |
| `scheduled` | The previous poll did not list the meeting. | As listed now. | Not set. |
| `updated` | The topic, type, start, duration, time zone, or listed agenda changed. | As listed now. | As listed by the previous poll. |
| `removed` | The meeting was deleted, or it expired and Zoom stopped listing it. | As listed by the previous poll. | Not set. |

- **Baseline.** The first poll stores the current list and delivers nothing.
- **Event IDs.** Each ID hashes the kind, meeting ID, both listed states, and
  the commit that recorded the previous state, so a refetch after a crash keeps
  the ID, and a meeting renamed back and forth gets a new ID each time. A
  `scheduled` ID also hashes the cursor's count of commits with changes, so a
  meeting recovered from Zoom's Recently Deleted list arrives again as
  `scheduled` with a new ID.
  `occurredAt` is the meeting's creation time for `scheduled` and the poll time
  otherwise, since Zoom reports no modification time.
- **Recurring meetings.** A recurring meeting is one entry. When Zoom lists a
  meeting ID more than once, such as one entry per occurrence, the entry with
  the earliest start counts, so the tracked start is the next listed
  occurrence, and its move to the following occurrence arrives as `updated`.
- **Pagination.** A poll stops after 35 pages, enough for the 10,000-meeting
  snapshot bound. Zoom expires `next_page_token` after 15 minutes, so a `400` on
  a later page restarts the listing once. A meeting listed twice counts once.
  A poll with more than 100 changes commits them 100 at a time.
- **Reset.** A stored cursor that is undecodable, or a listing whose meetings
  all belong to another Zoom user than the snapshot (the connection was
  reauthorized as someone else), records a new baseline, increments the
  cursor's `resetCount`, and logs `poll cursor invalid; reseeding` at WARN.
  Differences in that gap are not delivered.
- **Rate limits.** List meetings is a Medium API: 2/second and 2,000/day on
  Free accounts, 20/second on Pro, and 60/second on Business and above
  ([rate limits](https://developers.zoom.us/docs/api/rate-limits/)). Every 429,
  including the daily-limit 429 that a Step treats as `providerRejected`,
  pauses every binding of the connection for `Retry-After`, or backs off
  exponentially up to 15 minutes without it. Other failures back off per
  binding. The connection's `pollInterval` setting is at least 30 seconds.
- **Scopes.** The Trigger uses `meeting:read:list_meetings`, which the
  connection already requests. No new scope or reauthorization is needed.

`zoom.NewProjectMeetingTriggerRunner` loads saved bindings and records each
change in the binding's project Trigger inbox before the snapshot commits in
its project poll cursor, as
[`examples/meeting-changed/main.go`](go/examples/meeting-changed/main.go) does:

```go
triggerRunner, err := zoom.NewProjectMeetingTriggerRunner(project, meetingchanged.ConnectionName, zoom.ProjectMeetingTriggerRunnerConfig{
	MeetingChangedRoutes: []zoom.ProjectMeetingChangedTriggerRoute{{
		BindingName: meetingchanged.MeetingChangedTriggerBinding,
		Target: sdkgo.NewDexFlowTriggerTarget(client, flow, meetingchanged.AcceptEveryMeetingChange, meetingchanged.ResolveFlowID,
			meetingchanged.MapToFlowInput, sdkgo.WithTriggerLogger(logger)),
	}},
}, zoom.WithLogger(logger))
```

`zoom.NewMeetingTriggerRunner` takes targets and cursor stores directly, and
the generated `zoom.NewMeetingChangedTrigger` keeps the snapshot in memory, so
it records a new baseline on every start. `MeetingChangedTriggerConfiguration`
has no Dex Web field, because the binding always watches the authorized user;
an application may set `ChangeKinds` in code to skip kinds, which still advance
the snapshot. The [meeting-changed example](go/examples/meeting-changed/README.md)
reads each scheduled or updated meeting back with `getMeeting`.

## Mocks

The generated `zoommock` package provides one consistent consultation:
`createMeeting` and `getMeeting` return meeting `92674392836`, `updateMeeting`
updates that ID, and `listMeetings` and `listPastMeetingParticipants` serve
three pages each. Every operation has one default happy path plus mocks for
its definite business branches. The default pages include duplicates across
page boundaries so applications must handle changing list snapshots. Meeting
defaults include `createdAt` for reconciliation after an uncertain create.

The runnable example's real-Dex mock test binds those operations to the Flow:

```go
mock := zoommock.New(t, ConnectionName)
flow := NewFlow(mock.Connection(), &AttendancePolicy{CheckDelay: time.Hour})
```

See [`workflow_mock_integration_test.go`](go/examples/booked-meeting/flow/workflow_mock_integration_test.go)
for literal call-input, call-count, reschedule, and attendance assertions.

## Python package

The Python package is [`python/`](python/README.md), version `v0.2.0` with
`dex-connectors-sdk>=0.12.0`. It implements all five meeting operations using
the manifest-generated models, Step factories, connection, and OAuth refresh
wiring, and, from 0.2.0, the `meetingChanged` poll Trigger at parity with Go:
the same change kinds, event IDs, pagination, reset, and rate-limit rules, and
poll cursor documents that Go and Python replicas read and write byte for byte.
The checked-in [meeting-list example](python/examples/meeting_list/README.md)
binds `zoom-meetings`, and the
[meeting-changed example](python/examples/meeting_changed/README.md) starts one
Flow per change on `zoom-meeting-watcher`; both have real Dex Worker/Client
integration tests with a fake provider. `createMeeting` uses the SDK
single-dispatch fence and reports an unconfirmed dispatch as uncertain. No live
Zoom authorization or provider request was performed for this port.

## Not in this release

- **Webhook Triggers.** Zoom event subscriptions, such as `meeting.ended`,
  require a URL validation handshake before Zoom delivers events, which
  `sdkgo/webhooktrigger` does not support yet. Until then, the booked-meeting
  example ends its wait with a Timer or an Action, and `meetingChanged` polls.
- **Python and TypeScript Triggers.** `meetingChanged` is Go only.
- **Pickers.** No operation or example field takes a provider resource the user
  would pick: the host is always the authorizing user, and meeting IDs come
  from Flow data. The manifest declares no Studio bundle.
- **Server-to-Server OAuth**, as described under Token refresh.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the latest Dex development server running, the example's real Dex
integration tests cover Worker, retry, RPC, Channel, Timer, persistence, and
recovery behavior:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```

The meeting-changed integration tests run a real Dex Worker and Client against
a local List meetings stand-in: baseline, one Flow per change across a Trigger
process restart, and a reseed when the listing belongs to another Zoom user.

From the repository root, check the manifest and generated code:

```bash
go run ./cmd/connectorctl validate connectors/zoom/connector.yaml
go run ./cmd/connectorctl generate --check connectors/zoom/connector.yaml
```

The deterministic provider fakes cover every branch above, the request shapes,
the refresh exchange and rotation, credential reflection, redirects, oversized
and malformed responses, and the absence of start URLs, passcodes, and Zoom
message text in results.

The 503 retry and mock-backed example tests run on a real Dex stack in CI.
Local verification uses provider fakes and does not start Dex.

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
- the status and body Zoom returns for an expired `next_page_token`, which
  `meetingChanged` assumes is a `400`, and whether List meetings returns a
  recurring meeting once or once per occurrence, and with which `start_time`;
- whether Zoom consistently leaves a meeting uncreated after a 503 response,
  and whether its create endpoint offers any undocumented provider-side
  deduplication or lookup by a client-supplied value;
- real meeting, listing, and participant response shapes, and how Zoom handles
  a time zone outside its supported list;
- the Marketplace page paths in the setup guidance.
