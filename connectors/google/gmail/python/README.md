# Gmail connector for Python

`dex-connector-google-gmail` provides typed Gmail reads, sends, and replies for
Dex Python Flows. The package imports as `dex_connectors.google_gmail`.
Version 0.5.2 includes `getMessage`, `sendMessage`, and `replyToMessage`, and
the `messageReceived` and `replyReceived` Triggers at parity with Go. From
0.4.0 a send or reply whose outcome is unknown may be sent up to three times;
from 0.5.0 one that Sent Mail cannot confirm at that cap selects `uncertain`.
From 0.5.1 Sent Mail read-back matches the `X-Dex-Message-ID` header, because
Gmail replaces `Message-ID` on send, and `getMessage` rejects body data in the
standard base64 alphabet or with malformed padding, as Go and TypeScript do.
From 0.5.2 recipients, reply targets, `To` headers, and Trigger sender filters
are parsed by a port of Go's `net/mail` parser, and recipient display names are RFC 2047 encoded exactly
as Go writes them, split into words of at most 75 characters; see the
[connector README](../README.md).

## Install

After release, install `dex-connector-google-gmail==0.5.2`. The package requires
`dex-connectors-sdk>=0.12.0,<1` and `dex-python-sdk>=1.28.0`; the latter provides
Go-compatible JSON field metadata and the Flow start and attempt deadline
needed for Gmail's duplicate-send fence.

## Configure a connection

Use one static connection name in the Flow and in Dex Web **Connections**.
Select Google OAuth for a personal Gmail or ordinary Workspace mailbox. In
[Google Cloud Console](https://console.cloud.google.com/apis/credentials),
enable Gmail API, configure the consent screen, register the exact Dex redirect
URI on a Web application, then grant `openid`, UserInfo email, Gmail read-only,
and Gmail send scopes. Dex derives the verified primary email and stores the
access and refresh tokens in encrypted project storage. Its connection provider
coordinates token refresh and persists rotated refresh tokens. A service-account
key with Workspace domain-wide delegation is the administrator-only alternative.
The refresh driver reads generated manifest grant declarations, while Gmail
keeps its delegated mailbox and service-account key validation.

The Python connector uses the same manifest authorization guide and safe
defaults as the Go connector. Dex CLI/Web 1.28.0 supports Python Flow
analysis, and the package includes generated `dex-connector.json` for Dex Web
connection discovery. Dex Web authorization has not been live-verified with
this package. Do not put access tokens, refresh tokens, or service-account
keys in Flow input, results, Attributes, Streams, logs, or mock reports.

## Run the example

[`examples/thread_reply`](examples/thread_reply/main.py) reads one received
message, replies in its existing thread, and completes with the accepted reply
IDs. Its Worker loads the `gmail-inbox` connection from the Dex Web project
configuration. Factory calls keep their connection name and annotation fields
inline so Dex CLI can read the Flow definition; the factories also accept
`StepAnnotations` for programmatic construction. Dex CLI 1.28.1 is needed for
strict FDG analysis of this example's local Steps routing into connector Steps
(tracked in Dex issue #362). The entrypoint uses the public connection and Step
APIs:

```python
connection = new_project_connection(project, CONNECTION_NAME)
await run_thread_reply_worker(
    ThreadReplyFlow(connection),
    os.environ.get("DEX_BLOB_CACHE_DIRECTORY", "/tmp/dex-gmail-thread-reply-cache"),
    os.environ.get("DEX_SERVER_ADDRESS", "localhost:8801"),
)
```

`Connection` exposes each operation through a method, as generated connector
packages do: `connection.get_message()`, `connection.send_message()`, and
`connection.reply_to_message()`. Close a provider-backed connection with
`await connection.aclose()`. Version 0.2.0 replaced the earlier dataclass
fields and `close()` (breaking). `new_connection_with_operations` binds mock
or custom operations to the same `google` connection reference.

Run `dexcli dev`, then from this `python/` directory run
`DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json python -m examples.thread_reply.main`.
The Flow type is `ThreadReplyFlow`; its input JSON contains `messageId` and
`replyText`. Use a real mailbox message ID and a reply body with no secrets.
The [integration test](examples/thread_reply/tests/test_integration.py) runs
the same Flow on a real Dex Worker and Client with a local fake Gmail HTTP
boundary and manifest mocks.

Decoded `Message.to` and `SendMessageOutput.recipients` use Go-compatible RFC
mailbox strings, including angle brackets around each address.

## Receive messages and replies

The `messageReceived` Trigger receives a matching message that begins a Gmail
thread, and `replyReceived` a matching reply, one with an `In-Reply-To` or
`References` header. Each binding polls the newest inbox page every connection
**Poll interval**, inspecting **Poll page size** messages
(`labelIds=INBOX`, plus the binding's optional Gmail `searchQuery`), reads each
new message's metadata, and delivers matches oldest first as a `MessageEvent`:
the mailbox, message and thread IDs, sender, subject, snippet, `receivedAt`, and
`isReply`. The event ID is the Gmail message ID. A binding's `MessageMatcher`
keeps messages whose subject or snippet contains `messageContains`, ignoring
case, and whose sender address is in `senderEmails`; both may be blank.

Run every binding of one connection on one runner, so each poll lists replies
before roots and then delivers every root before any reply. A root therefore
starts its Flow before a reply to it arrives:

```python
runner = new_project_message_trigger_runner(
    project,
    connection,
    message_received_routes=[ProjectMessageReceivedTriggerRoute(START_TRIGGER_BINDING, start)],
    reply_received_routes=[ProjectReplyReceivedTriggerRoute(REPLY_TRIGGER_BINDING, reply)],
)
await runner.run(TriggerContext(cancellation))
```

`new_project_message_trigger_runner` loads each binding's saved configuration
and keeps acknowledged events in the binding's project Trigger inbox until its
target consumes them, so a restarted replica replays them before it polls. Go
and Python replicas share the inbox. `new_message_trigger_runner` takes
configurations and targets directly, and `new_message_received_trigger` and
`new_reply_received_trigger` poll one binding each, as Go's generated factories
do.

Delivery stops at the first failure. Every later poll retries that event before
any listed message, even after newer mail pushes it off the page, and an event
a target marks undeliverable is consumed. The delivered set covers the last
listed page in memory, so a restart rescans the page; a deterministic Flow ID
with the event ID as the start request ID, and an RPC handler that records the
accepted event ID, absorb the duplicates. Poll credentials use the Go
connector's stable call ID for each connection and Trigger.

Pass `logger=` to `new_project_connection` or `GmailProvider` to receive the
Trigger records; the default is the `dex_connectors.sdk.trigger` standard
logger. WARN `gmail poll failed; retrying` carries the consecutive failed polls
as `attempt` and the poll interval as `delay_ms`; WARN `trigger delivery
failed; retrying`, INFO `trigger delivered after retry`, and DEBUG `trigger
event ignored` with its `reason` (`not_a_root`, `not_a_reply`, or
`matcher_mismatch`) follow Go. Records carry message and thread IDs and error
types, never senders, subjects, snippets, bodies, queries, or tokens.

[`examples/trigger_thread_reply`](examples/trigger_thread_reply/README.md) runs
both Triggers against a real Dex Worker. Live Gmail polling has not been
verified with this package.

## Send confirmation

Gmail does not deduplicate messages by `Message-ID`. The connector derives a
stable run-specific RFC Message-ID from the Dex Call key and run ID. A valid Flow
start time contributes its Unix second, matching Go; an absent or invalid start
time uses `no-flow-start`. From 0.5.1 every send and reply repeats that value in
an `X-Dex-Message-ID` header directly after `Message-ID`, beside the
`X-Dex-Call-ID` correlation header. Gmail's `messages.send` replaces the client
`Message-ID` on the Sent copy with its own `<…@mail.gmail.com>` value, also for
an ID in the sender's own domain, and keeps no copy of it, while it keeps the
`X-Dex-*` headers verbatim. Gmail search cannot match a custom header, so
read-back lists up to 25 recent Sent Mail candidates with
`in:sent newer_than:2d`, adding `to:` and the first recipient when the recipient
is known and its address uses only letters, digits, and `.`, `_`, `+`, or `-`. A
reply's read-back before its source read knows no recipient, so below the send
cap a later attempt reads back again with the source's reply recipient before it
posts; at the cap only the read-back without a recipient runs. The connector then
reads each candidate's `Message-ID` and `X-Dex-Message-ID` with
`format=metadata`. Only a candidate whose `X-Dex-Message-ID`, or whose
`Message-ID` where a compatible gateway kept it, equals this call's value is a
match; a Sent copy of another call, or of an earlier run of a reused Flow ID
with the same `X-Dex-Call-ID`, is not. Within one poll a candidate found to be
another message is not read again, a candidate deleted after the search is a
miss, and a candidate whose read fails leaves the search inconclusive unless
another candidate matches. A credential refreshed after a 401 during read-back
serves that read-back's remaining reads and every later request before the POST.
Up to 0.5.0 read-back searched `rfc822msgid:`, which
never matches the rewritten `Message-ID`, so a crash or lost answer after Gmail
accepted a send was resent up to the send cap. Two gaps remain: a Sent copy
outside that window or candidate limit, or one deleted from Sent Mail, is not
found, so the send is resent up to the cap; and a message posted by 0.5.0 or
earlier carries no `X-Dex-Message-ID`, so a call whose attempts cross the
upgrade cannot be confirmed. A missing run ID returns a local defect before any
Gmail request. Its Python Mutation Steps use the manifest's synchronous Execute
durability and persist the single-dispatch heartbeat before a POST. On a
numbered retry it searches Sent Mail first. After a possibly accepted POST
loses its answer or returns 5xx, the connector polls Sent Mail before returning
Retry. A successful 2xx is already accepted even if its response is malformed,
oversized, or unreadable; a Sent Mail miss then returns `sent` with an empty
message ID. Both operations declare `repeatOnUnknown: limited` with
`maxSends: 3`, and the Fence takes `Call.max_sends`. After a claimed send whose
outcome stayed unknown, a Sent Mail miss or failed search resends with the same
Message-ID while fewer than that many sends may have taken effect. From 0.5.0
both operations declare an optional `uncertain` branch: when the final send the
cap allows stays unknown and Sent Mail finds no match, or a later attempt at the
cap, or with a saved message ID, cannot confirm the send, the attempt selects
`uncertain` without another POST; 0.4.x returned Retry until the retry budget
failed the Flow. A confirmed read-back still selects `sent`. A saved Gmail
message ID and an allowlisted 4xx refusal are never resent. An allowlisted 4xx
refusal, or a rejected or unusable reply source read, selects its branch only
when no earlier send is possible. Otherwise the earlier send may still be
delivered: an allowlisted 4xx selects `uncertain`, and an unusable source read
returns Retry while the cap permits another send and selects `uncertain` when
this attempt's send would be the last. A POST status of 400, 401,
403, 404, 411, 413, 414, 415, or 422 with no earlier possible send proves Gmail
refused it before processing, unless its body is unreadable or names a Gmail
message, so it records `canResend` and selects `providerRejected`. Any other
4xx except 429, such as 408 or 409, a contradictory refusal body, or an
unreadable or untrusted checkpoint, leaves the outcome unknown: below the cap it
returns Retry and keeps the claim, so the next attempt reads back Sent Mail and
resends; on the final send the cap allows it selects `uncertain`. After an
earlier possible send, `defect` is never selected: invalid input selects
`uncertain`, a transient credential failure returns Retry,
and one that needs reconnection selects `uncertain`. From 0.5.0, a refusal body that names a Gmail message is read
back by that ID, which is checkpointed so later attempts never resend it, and a
transient credential failure before any send records `notSent` and returns
Retry instead of selecting `defect`. A Step write window that cuts the POST
body after its headers keeps the observed status for classification.
A 503 is treated like any other 5xx. A definite 429 refusal, or a
transport failure proven to precede dispatch, permits a later send. The
manifest limits sends and replies to seven attempts within five minutes;
`max_sends_override` on the generated Step factory changes one Step's cap.

A live Google Workspace mailbox showed the `Message-ID` replacement and the
verbatim `X-Dex-Message-ID` that read-back relies on. This package has not run
against a live account, so live provider authorization and Sent Mail indexing latency
remain unverified. No live provider credentials are included in this package.
