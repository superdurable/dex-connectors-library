# Microsoft Teams incident acknowledgement example

This example runs one operation-only Flow from Dex Web **Start Flow** and uses
every Microsoft Teams operation to get a person to acknowledge an incident:

1. `RecordIncidentUpdate` validates the incident and records it.
2. `PostIncidentUpdate` calls `teams.NewPostChannelMessageStep` once with the
   incident as the subject and the summary as HTML, in the channel the team
   and channel pickers saved. `providerRejected` completes as `rejected`;
   `uncertain` completes as `postOutcomeUnknown` and is never re-sent.
3. `PostStatusReply` calls `teams.NewPostThreadReplyStep` with
   `Status: SEV2 incident INC-1042 is being investigated. Reply ack in this
   thread to acknowledge.` Every outcome is recorded and the Flow goes on.
4. `WaitForAcknowledgement` waits on a durable Timer, then
   `ReadThreadReplies` calls `teams.NewListThreadRepliesStep` for the newest
   50 replies. `CheckAcknowledgement` completes as `acknowledged` on the
   earliest reply by a person other than the connected account whose text
   contains the phrase as whole words, case-insensitively. Deleted replies, app
   and system messages, and the status reply never count, and `ack` does not
   match `back` or `acknowledged`.
5. After the last read with no acknowledgement, `EscalateToChat` calls
   `teams.NewPostChatMessageStep` once with an urgent message in the picked
   chat, and the Flow completes as `escalated`. With no chat picked it
   completes as `unacknowledged`.
6. When replies cannot be read, for example because nobody granted
   administrator consent for `ChannelMessage.Read.All`, the Flow completes as
   `repliesUnreadable` with the failure kind.

The Worker reads every 30 seconds, at most 20 times. Each poll reads only the
newest 50 replies, so an acknowledgement is missed only if more than 50 replies
arrive between two reads. Unwired optional branches, such as `defect`, fail
the Flow.

## Configure

Follow the [Microsoft Teams connector setup](../../README.md); Microsoft OAuth
needs Dex CLI 1.4.1 or later. Configure the `microsoft-teams` connection under
**Connections** in Dex Web, then on the `PostIncidentUpdate` Step save the
**Incident team** and **Incident channel** pickers. The Worker does not start
until both are saved. Optionally save the **Escalation chat** picker on
`EscalateToChat`. The Worker reads the saved values once at startup, so
restart it after saving.

Use a dedicated account for the connection: its own replies never count as an
acknowledgement, and Teams shows it as the sender of every message.

## Run

Generate strict FDG 2.0 from `connectors/microsoft/teams` with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/incident-acknowledgement/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/incident-acknowledgement
```

Before the first connector release the graph reports only the expected
`connector_release_required` warning on the four Teams Steps. Run `dexcli dev`
with that build directory, then run the Worker. It reads the `DEX_PROJECT_*`
project configuration environment documented in
[`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md#application-loading);
Dex Web or Superverse Studio writes that configuration:

```bash
go run ./examples/incident-acknowledgement
```

The default Worker address is `127.0.0.1:8883`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed.

Start `TeamsIncidentAcknowledgement` with a unique Flow ID:

```json
{
  "incidentId": "INC-1042",
  "title": "Checkout latency above SLO",
  "severity": "SEV2",
  "summary": "p95 checkout latency is 4.2 s and rising.\nOwner: payments on-call",
  "acknowledgementPhrase": "ack"
}
```

A blank `acknowledgementPhrase` means `ack`. The Flow result and the
`teams-incident` Attribute hold the phase, the post and status reply IDs, the
number of reply checks, the acknowledging person, and any escalation.

## Test

```bash
GOWORK=off go test -race ./examples/incident-acknowledgement/...
```

With the latest Dex development server running, the integration tests drive
the Flow on a real Worker against a stateful fake Microsoft Graph: an incident
posted, replied to, and acknowledged by a person after a non-acknowledging
reply and the poster's own `ack`; a channel post and a reply that each take nine
seconds, sent once under sync durability; a post whose response never arrives,
found by reading the channel back without a second post; a lost post with no
match, completed as `postOutcomeUnknown` without a resend; a Worker process
killed mid-post and replaced, which reads back instead of posting again; an
unacknowledged incident escalated once with a nine-second chat message; replies
forbidden without administrator consent; and a rejected channel post.

```bash
GOWORK=off go test -tags=integration ./examples/incident-acknowledgement/... -count=1 -v
```
