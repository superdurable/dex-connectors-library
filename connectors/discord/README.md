# Discord Connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/discord/go@v0.4.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/discord`.

Read Discord channels and messages, send bot messages, and add reactions
through the [Discord HTTP API](https://discord.com/developers/docs/reference)
with typed Dex Query and Mutation Steps. Use the
[channel announcement example](go/examples/channel-announcement/README.md) to post
an announcement, react to it, and confirm it with a read-back.

Its Go module is `github.com/superdurable/dex-connectors-library/connectors/discord/go`.
The connector ID is `discord`.

The TypeScript package is `@superdurable/connector-discord` at v0.2.0. It supports all four operations and no Triggers. See the [TypeScript package](typescript/README.md) and its [runnable announcement example](typescript/examples/channel-announcement/README.md).

## Operations

| Factory | Kind | Input | Required branch | Optional branches |
| --- | --- | --- | --- | --- |
| `NewGetChannelStep` | Query | `GetChannelInput` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `NewListChannelMessagesStep` | Query | `ListChannelMessagesInput` | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `NewCreateMessageStep` | Mutation | `CreateMessageInput` | `created` | `notFound`, `providerRejected`, `invalidResponse`, `uncertain`, `defect` |
| `NewAddReactionStep` | Mutation | `AddReactionInput` | `added` | `notFound`, `providerRejected`, `defect` |

- `getChannel` reads `GET /channels/{channel.id}` and returns the channel's ID,
  type number, server ID, name, topic, and newest message ID.
- `listChannelMessages` reads one page of up to 100 messages (zero asks for
  Discord's 50), newest first. Set at most one of `BeforeMessageID`, such as the
  previous page's `OldestMessageID`, and `AfterMessageID`. `IsFull` reports a
  page that holds the limit, so an older page may exist.
- `createMessage` sends 1 to 2000 characters of text, optionally as a reply to
  a message in the same channel. Every mention is disabled with
  `allowed_mentions: {parse: []}`, so `@everyone` or `<@id>` notifies nobody.
- `addReaction` adds the bot's reaction with a Unicode emoji, such as `👍`, or
  a custom emoji written `name:id`.

Channel, message, and emoji IDs are Discord snowflakes of 17 to 20 digits; any
other value selects `defect` before a request, so input can never change the
request path. Message `Content`, channel `Name`, and `Topic` are untrusted
provider text: escape them before rendering HTML. A bot without the Message
Content intent reads empty content for server messages that do not mention it.

Use the generated factories in [the runnable Flow](go/examples/channel-announcement/flow/workflow.go):

```go
		dex.DefineStep(discord.NewCreateMessageStep(discord.CreateMessageStepConfig[AnnouncementRequest]{
			StepType: postAnnouncementMessageStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "announcement-post", GroupLabel: "Announcement post", Explanation: "Post the announcement once, deduplicated by a Discord nonce across retries."},
			Connection:          flow.connection,
			MapToOperationInput: MapToCreateMessageInput,
			Created:             sdkgo.GoTo(announcementPosted{}),
		})),
```

The Worker opens the connection from the project configuration that
`projectconfig.LoadFromEnvironment` loads:

```go
	connection, err := discord.NewProjectConnection(project, channelannouncement.ConnectionName)
	if err != nil {
		return err
	}
```

`New` and `NewConnection` build a connection for tests, and `WithHTTPClient`
supplies a transport.

## Execution contract

Every operation uses a 30-second Execute timeout. `createMessage` uses sync durability so its single-dispatch fence survives retries and Worker loss, and keeps five attempts within two minutes; the other operations use async durability and the one-hour default retry budget. Each attempt makes one HTTP request, except that a `createMessage` attempt may read an accepted message back with one more GET. Redirects are not followed.

`createMessage` sends a 25-character `nonce` derived with SHA-256 from the
Step's idempotency key, with `enforce_nonce: true`. Retries and Worker restarts
of one Step execution send the same nonce, and inside its nonce window Discord
returns the message the earlier attempt created instead of posting another.
Without a known message ID, that resend is the read-back. The
operation is `repeatOnUnknown: limited` with `maxSends: 3`: an unknown outcome
after dispatch, such as a lost connection, a 5xx other than 503, a 408, or an
unlisted status, resends while possible sends stay below the cap and selects
`uncertain` at the cap. A 2xx proves Discord created the message, so it is
never resent. A 2xx whose body is broken, oversized, or cut by the write window
selects `uncertain`. A 2xx whose body parses but is not a valid message selects
`uncertain` unless it carries a valid snowflake `id`. With such an ID, the
operation checkpoints it and reads the message back with
`GET /channels/{channel}/messages/{id}`. A matching message selects `created`.
A transient read-back failure (a lost connection, a broken or oversized body,
408, 429, or 5xx) retries, and the next attempt reads back the saved ID again
without sending; if the retry budget runs out first, the Flow fails. If the ID
could not be checkpointed, that failure selects `uncertain` instead. Every other
read-back answer selects `uncertain`: a complete 2xx that is not the matching
message, 404 Unknown Message, a missing Read Message History permission, or any
other status. This differs from Gmail, whose Sent Mail search selects
`uncertain` at the send cap instead of retrying. A timestamp without an offset
is read as UTC. One residual risk remains: a Worker lost between the 2xx and the
message ID checkpoint leaves only the claim, so the next attempt resends with
the same nonce, still within the send cap, because nothing proves the first POST. A 429, a 503, or a request that never left the process retries and never
selects `uncertain` on its own. A definite refusal of the first send selects `notFound` or
`providerRejected` and is never resent; the same refusal after an earlier
possible send selects `uncertain`. An application sets the cap, from 1 to 10,
with the factory's `MaxSends`. Discord enforces a nonce for only a few minutes, so the
retry window stays at two minutes; a `StepOptionsOverride` that lengthens it
can post a duplicate. A new Step execution is a new message.

`addReaction` sends no key: Discord keeps one reaction per user and emoji, so
repeating the PUT converges.

| Discord response | Outcome | Failure kind |
| --- | --- | --- |
| 2xx | happy-path branch | none |
| 404 with code 10003, 10008, 10014, or no code | `notFound` | `NOT_FOUND` |
| 401 | `providerRejected` | `AUTHENTICATION` |
| 403, such as 50001 Missing Access or 50013 Missing Permissions | `providerRejected` | `AUTHORIZATION` |
| 400 or 413, such as 50035 Invalid Form Body or 30010 maximum reactions | `providerRejected` | `VALIDATION` |
| 429 | Retry after the body's `retry_after`, else `Retry-After` | `RATE_LIMIT` |
| 408, any 5xx | Retry, honoring `Retry-After`; for `createMessage`, `uncertain` at the send cap except 503 | `AVAILABILITY` |
| transport failure, interrupted body | Retry; for `createMessage` after dispatch, `uncertain` at the send cap | `TRANSPORT` |
| other 404, 411, 414, 415, or 422 | `providerRejected` | `PROVIDER_REJECTION` |
| 3xx or any other status | Retry; for `createMessage`, `uncertain` at the send cap | `AVAILABILITY` |
| malformed JSON, a different channel, more messages than the limit, an oversized body | `invalidResponse` for a Query; Retry for `addReaction`; for `createMessage`, a read-back by a valid message ID, otherwise `uncertain` without a resend | `PROTOCOL`, `RESPONSE_TOO_LARGE`, or `TRANSPORT` |

For `createMessage`, a refusal status counts as definite only when its body is
readable and does not describe a message. An error body over 64 KiB, or one that
is contradictory, is an unknown outcome. `invalidResponse` stays declared for compatibility
and is no longer selected by either write. `addReaction` is declared
`repeatOnUnknown: safe` and repeats an idempotent PUT.

Stored credential material that does not decode, such as a token whose stored
form is corrupt, is handled differently by language. In Python and TypeScript it
selects `defect` before any request. In Go the SDK does not yet expose the
decoder error type, so the operation retries without a request until the retry
budget ends. A decoded token that is blank or unsafe in a header selects `defect`
in every language. After an earlier possible send, `createMessage` selects
`uncertain` instead of `defect`.

A Failure names only a documented numeric Discord error code, never Discord's
message text. An unwired optional branch fails the Flow. The connector has no
Trigger: receiving new messages needs the Discord Gateway or a polling source,
which is left for a later release.

| Configuration | Default | Meaning |
| --- | --- | --- |
| `endpoint` | `https://discord.com/api/v10` | API base URL including the version; HTTPS, or HTTP for a loopback test server |
| `maxResponseBytes` | `4194304` | Maximum response body size in bytes |

## Mocks

`discordmock` provides one default answer per operation for a single announcement in channel
`100000000000000001`. The create and list defaults share message `300000000000000001`,
so the runnable Flow confirms its own post. Script a different case when the input
names another channel, message, or text. Optional cases cover unknown resources
and permission denials. See [connector mocks](../../sdkgo/README.md#connector-mocks).

The [runnable real-Dex test](go/examples/channel-announcement/flow/workflow_mock_integration_test.go)
uses the defaults without Discord credentials:

```go
mock := discordmock.New(t, ConnectionName)
flow, harness := newAnnouncementIntegrationHarnessWithConnection(t, mock.Connection())
harness.startWorker(t)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
defer cancel()
request := AnnouncementRequest{ChannelID: "100000000000000001", Text: "Release today @everyone"}
result := harness.runFlow(t, ctx, flow, "discord-mock-"+strconv.FormatInt(time.Now().UnixNano(), 10), request)
require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
```

## Security

The bot token is a `secretString` credential resolved for every call, so a
reset token applies without restarting the Worker. A reauthorization-required
connection selects `providerRejected`; a missing or unselected connection, a
credential broker rejection, or a blank or header-unsafe token selects `defect`;
undecodable stored material follows the language rule in the execution contract;
every other credential failure, such as an unavailable broker, retries without a request.
The token travels only in the
`Authorization: Bot <token>` header and never in a URL, Result, Failure,
receipt, or log. Create it under **Bot > Reset Token** at
[discord.com/developers/applications](https://discord.com/developers/applications)
and store it only in Dex Web **Connections**. Grant the bot only View Channels,
Read Message History, Send Messages, and Add Reactions in the channels it uses.

## Verification

From `connectors/discord`:

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/channel-announcement/... -count=1
```

The provider tests run every operation against an `httptest` fake Discord API.
They cover the `Bot` authorization and `DiscordBot` User-Agent headers, the
stable enforced nonce across resends of one Step execution and a new nonce for
a new execution, `uncertain` at the send cap, refusals that are never resent,
read-back by message ID after an accepted but invalid answer,
disabled mentions, reply references, emoji path escaping,
page bounds and cursors, every error class, fractional rate-limit delays,
malformed and oversized responses, input and credential validation, and
secret-safe Failures. The example's real Dex suite covers the complete
announcement, a post whose first response was lost, an accepted post read back
by message ID, an unwired `notFound`, and
invalid start input.

No test runs against the real Discord API yet.

## Python package

The `python/` package implements all four operations with generated typed models and Step factories. Its runnable [channel announcement example](python/examples/channel_announcement/README.md) uses a static `discord-announcements` connection and a Dex Worker. Run `uv sync && uv run pytest` from `python/`. Go-recorded conformance scenarios cover provider requests, branches, retry, bounded resend up to `uncertain`, and the stable message nonce. Live Discord authorization and API behavior remain unverified for the Python port.
