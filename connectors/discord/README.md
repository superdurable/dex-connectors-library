# Discord Connector

Read Discord channels and messages, send bot messages, and add reactions
through the [Discord HTTP API](https://discord.com/developers/docs/reference)
with typed Dex Query and Mutation Steps. Use the
[channel announcement example](examples/channel-announcement/README.md) to post
an announcement, react to it, and confirm it with a read-back.

Its Go module is `github.com/superdurable/dex-connectors-library/connectors/discord`.
The connector ID is `discord`.

## Operations

| Factory | Kind | Input | Required branch | Optional branches |
| --- | --- | --- | --- | --- |
| `NewGetChannelStep` | Query | `GetChannelInput` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `NewListChannelMessagesStep` | Query | `ListChannelMessagesInput` | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `NewCreateMessageStep` | Mutation | `CreateMessageInput` | `created` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
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

Use the generated factories in [the runnable Flow](examples/channel-announcement/flow/workflow.go):

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

Every operation uses async durability, a 30-second Execute timeout, and five
attempts within two minutes, with exponential backoff from 1 second to 30
seconds. Each attempt makes one HTTP request. Redirects are not followed.

`createMessage` sends a 25-character `nonce` derived with SHA-256 from the
Step's idempotency key, with `enforce_nonce: true`. Retries and Worker restarts
of one Step execution send the same nonce, and Discord returns the message the
earlier attempt created instead of posting another. A transport failure, a 5xx,
or a 429 after dispatch is therefore retried rather than reported as uncertain.
Discord enforces a nonce for only a few minutes, so the retry window stays at
two minutes; a `StepOptionsOverride` that lengthens it can post a duplicate. The
operation declares no `uncertain` branch. A new Step execution is a new message.

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
| 408, 5xx other than 501 | Retry, honoring `Retry-After` | `AVAILABILITY` |
| transport failure, interrupted body | Retry | `TRANSPORT` |
| any other status | `providerRejected` | `PROVIDER_REJECTION` |
| malformed JSON, a different channel, more messages than the limit, an oversized body | `invalidResponse` (`providerRejected` for `addReaction`) | `PROTOCOL` or `RESPONSE_TOO_LARGE` |

A Failure names only a documented numeric Discord error code, never Discord's
message text. An unwired optional branch fails the Flow. The connector has no
Trigger: receiving new messages needs the Discord Gateway or a polling source,
which is left for a later release.

| Configuration | Default | Meaning |
| --- | --- | --- |
| `endpoint` | `https://discord.com/api/v10` | API base URL including the version; HTTPS, or HTTP for a loopback test server |
| `maxResponseBytes` | `4194304` | Maximum response body size in bytes |

## Security

The bot token is a `secretString` credential resolved for every call, so a
reset token applies without restarting the Worker. It travels only in the
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
stable enforced nonce across retries of one Step execution and a new nonce for
a new execution, disabled mentions, reply references, emoji path escaping,
page bounds and cursors, every error class, fractional rate-limit delays,
malformed and oversized responses, input and credential validation, and
secret-safe Failures. The example's real Dex suite covers the complete
announcement, a post whose first response was lost, an unwired `notFound`, and
invalid start input.

No test runs against the real Discord API yet.
