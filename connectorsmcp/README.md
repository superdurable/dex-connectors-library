# Dex Connectors MCP server

`dex-connectors-mcp` serves the agent-exposed operations of Dex connectors as
[MCP](https://modelcontextprotocol.io) tools over stateless Streamable HTTP on
a loopback address. Tools run with the connections a developer authorized in
Dex Web, so an agent host such as a Dex agent's `McpToolSource` can call them
without holding credentials.

- Only operations whose manifest sets `agent.exposed: true` are offered, for
  connections present in the loaded project configuration.
- Tool definitions and results follow the
  [Dex MCP conventions](https://github.com/superdurable/dex/blob/main/agents/python/core/README.md#dex-mcp-conventions).
- A declared write runs at most once per `io.superdurable.dex/idempotencyKey`.
  The key's state persists across restarts, so a retried call replays its
  result or answers `unknown` and never sends blind.
- Writes without the key are refused. A client that does not follow the Dex
  conventions can only read.

The module is `github.com/superdurable/dex-connectors-library/connectorsmcp`
and the binary is `cmd/dex-connectors-mcp`. It requires Connector Go SDK
`sdkgo` v0.36.0 or later: v0.35.0 adds the `AgentTool` API, and v0.36.0 the
connection target guard. On `main`,
`cmd/dex-connectors-mcp/zz_generated_registry.go` links no connector; the
release workflow replaces it and `go.mod` with every connector that has an
exposed operation, at its latest complete Go release. Tests use fake
connectors built with `sdkgo.NewQueryAgentTool` and
`sdkgo.NewMutationAgentTool`.

## Commands

```text
dex-connectors-mcp serve [flags]
dex-connectors-mcp version [--output text|json]
dex-connectors-mcp list-tools [--connections-file PATH] [--connection ...|--connections-config PATH] [--output text|json]
```

`list-tools` builds the tool set once without listening.

### serve flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--listen HOST:PORT` | `127.0.0.1:8803` | Loopback only: `127.0.0.0/8`, `::1`, or `localhost`. Port `0` picks a free port. |
| `--connections-file PATH` | see below | Dex Web `connections.json`. |
| `--connection connectorId/connectionName[=tool,...]` | none | Enables a connection, optionally narrowed to tools. Repeatable. |
| `--connections-config PATH` | none | The same selection as a JSON file. It cannot be combined with `--connection`. |
| `--state-dir PATH` | `$HOME/.dex/connectors-mcp` | Call state, `server.lock`, and `server.json`. |
| `--allowed-host NAME` | none | An extra accepted `Host`, such as a local reverse proxy's. Repeatable. |
| `--call-time-budget DURATION` | `5m` | The time one call may spend on inline retries. |
| `--call-state-retention DURATION` | `720h` | How long write records are kept. At least `24h` and every exposed operation's retry window. |
| `--max-result-bytes N` | `65536` | The longest result text before truncation. At least 1024. |
| `--log-format text\|json` | `text` | The log format on stderr. |
| `--output text\|json` | `text` | The ready announcement format on stdout. `dexcli` uses `json`. |
| `--exit-on-stdin-close` | off | Exit gracefully when stdin reaches end of file, so a parent's death stops the server. |

The connections file is chosen in this order:

1. `--connections-file`.
2. `DEX_CONNECTOR_CONFIG_FILE`. Setting it together with a `DEX_PROJECT_*`
   variable is an error.
3. `$HOME/.dex/connectors/connections.json` when it exists.
4. The hosted project of the `DEX_PROJECT_*` variables, loaded once. Hosted use
   is untested in this phase. A hosted project offers only read tools; see
   [Connection rebinding](#connection-rebinding).
5. Otherwise the `$HOME` default, which is watched until it appears.

Every path is made absolute and cleaned.

Exit codes:

- `0`: a clean stop.
- `1`: a runtime failure.
- `2`: a usage error or invalid configuration.
- `3`: another `serve` holds the state directory. The message names its URL.

### Selection

The selection has the shape of the Dex CLI configuration: `connectorId/connectionName`
plus an optional tool list. Tool list elements are the snake-case operation name
(`post_thread_reply`, the `io.superdurable.connectors/operationName` value) or
the `operationId` (`postThreadReply`). Only an absent tool list (or `null` in the
file) offers every exposed tool. The flag form splits at the first `/`, then at
the first `=`, then on `,`, so a connection name with a comma needs the file
form.

```json
{
  "schemaVersion": "dex-connectors-mcp/connections-config/v1",
  "connections": [
    {"connection": "slack/work"},
    {"connection": "slack/personal", "tools": ["post_thread_reply", "list_thread_messages"]}
  ]
}
```

Malformed input is a usage error (exit code 2) that names the entry:

- a bad `connectorId/connectionName`;
- a repeated connection;
- an empty tool list, as `--connection slack/work=` or `"tools": []`. This
  fails closed, as agreed with Dex;
- an unknown `schemaVersion` or member;
- data after the JSON document.

Other problems skip only the affected entry and add a line to `warnings`:

- a connector this build does not include;
- a tool that is not exposed;
- a connection that is not authorized;
- a tool listed twice. It is kept once.

The server only narrows a selection. It never offers an operation that is not
exposed.

`connectionsConfigDigest` identifies the selection. `dexcli` computes it the
same way:

1. Build the normalized selection: one entry per connection,
   `{"connection": "<connectorId>/<connectionName>", "tools": [...] | null}`.
   `tools` is `null` when no list is given. Otherwise it holds
   the given strings exactly as written, de-duplicated and sorted by byte
   order. Entries are sorted by `connection`.
2. Encode `{"connections": [...]}` as canonical JSON: sorted keys, no
   insignificant whitespace, and no HTML escaping. Like any Go encoder, it
   writes U+2028 and U+2029 as `\u2028` and `\u2029`.
3. The digest is `"sha256:"` followed by the lowercase hex SHA-256 of those
   bytes.

Without a selection the digest is `null`. Flags and a file that select the
same connections give the same digest.

### Tool names

- The base name is `<connectorId>_<operationName>`, such as
  `hacker-news-daily_list_story_ids`. `operationName` uses connectorctl's
  snake case, which also names the Python factories.
- When a connector has more than one enabled connection, every tool of that
  connector becomes `<connectorId>_<connectionSlug>__<operationName>`, such as
  `slack_work__post_thread_reply`. `connectionSlug` replaces each character
  outside `[A-Za-z0-9_-]` with `_`.
- With a selection, the enabled connections are the configured entries of that
  connector, authorized or not. Without one, they are the authorized
  connections.
- A name longer than 64 characters keeps its first 55 characters, then `_` and
  8 hex characters of the SHA-256 of the full name.
- When two tools would get one name, neither is offered and a warning names
  them.

Adding a connection renames that connector's tools. A retry of an earlier call
under the old name is still answered, because its key's state is looked up
before the name. Filter tools by the `_meta` identity keys below, never by
parsing names.

## Readiness and discovery

`serve --output json` prints exactly one line to stdout after the listener is
bound and the tool set is built. Logs go to stderr only.

```json
{"type":"ready","url":"http://127.0.0.1:53121/mcp","healthUrl":"http://127.0.0.1:53121/healthz","version":"0.1.3","launchContract":1,"instanceId":"5b0c…","pid":4242,"stateDir":"/Users/me/.dex/connectors-mcp","connectionsFile":"/Users/me/.dex/connectors/connections.json","connectionsConfigDigest":null,"toolSetDigest":"sha256:…","tools":["slack_post_thread_reply"],"warnings":[]}
```

| Field | Meaning |
| --- | --- |
| `type` | `ready` |
| `url`, `healthUrl` | The MCP endpoint (`/mcp`) and the health endpoint (`/healthz`) on the bound address. |
| `version` | The server version. |
| `launchContract` | `1`. It changes only when the flags, this line, `server.json`, `/healthz`, or the state format change incompatibly. |
| `instanceId` | A UUID per process. `/healthz` reports the same value. |
| `pid` | The process ID. |
| `stateDir` | The absolute state directory. |
| `connectionsFile` | The absolute, cleaned connections file, or `null` when hosted. |
| `connectionsConfigDigest` | See [Selection](#selection). |
| `toolSetDigest` | Changes whenever the offered tools or their metadata change. |
| `tools` | The offered tool names, sorted. |
| `warnings` | Skipped selection entries and other non-fatal problems. |

A fatal startup error prints `{"type":"error","message":"..."}` instead and
exits non-zero.

`server.json` in the state directory holds the same object with mode `0600`.
It is written before the ready line is printed, rewritten when a reload
changes the tools, the warnings, or the selection, and removed on a clean
exit. `server.lock` is held for the process lifetime.

A launcher may reuse a running server only when all of these hold:

- its pid is alive;
- `/healthz` returns the same `instanceId`;
- its `launchContract` is supported;
- `connectionsFile` is equal;
- `connectionsConfigDigest` is equal. Two `null` values count as equal.

`GET /healthz` returns
`{"status":"ok","version":"…","launchContract":1,"instanceId":"…","toolCount":3}`.
It has the same Host and Origin checks as `/mcp` and names no tool or
connection.

### Hot reload

Before each request, and every 5 seconds, the server fingerprints three files
by size, modification time, mode, and SHA-256:

- `connections.json`;
- `use-configurations.json` beside it;
- the `--connections-config` file.

A change builds a new tool set with `projectconfig.LoadDexWebConnectionsFile`.
A keyed call resolves its tool from the tool set current once it holds the
key's lock. Every attempt, read or write, resolves its tool again before it
runs; see [Connection rebinding](#connection-rebinding). A credential that this
process writes back reloads to an equal tool set, so it is not reported.

A hosted project is loaded once and needs a restart for changes.

A connections file that is missing or fails to load serves no tools and adds a
warning.

## Tool definitions

Every tool carries `name`, `title` (`<displayName>: <first sentence of the
description>`), `description`, and `inputSchema`, the JSON Schema 2020-12 of
the operation input from the generated metadata. Its `_meta` holds these keys:

| Key | Value |
| --- | --- |
| `io.superdurable.dex/effect` | `read` for a Query, `idempotent_write` for a Mutation with `agent.retry: safe`, otherwise `non_idempotent_write`. |
| `io.superdurable.dex/approval` | `recommended`, only when the manifest says `agent.approval: recommended`. Otherwise the key is absent, because Dex asks for approval whenever the key is present. |
| `io.superdurable.connectors/connector` | The connector ID (manifest `metadata.name`), as in `--connection`. |
| `io.superdurable.connectors/connection` | The Dex Web connection name, exactly as in `connections.json`. |
| `io.superdurable.connectors/operation` | The manifest `operationId`, such as `getMessage`. |
| `io.superdurable.connectors/operationName` | The snake-case operation name, such as `get_message`. |
| `io.superdurable.connectors/connectorVersion` | The linked Go module version, such as `v0.26.0`. |

Annotations agree with the effect for clients that ignore `_meta`. Every tool
has `openWorldHint: true`. In addition:

- `read`: `readOnlyHint: true`.
- `idempotent_write`: `idempotentHint: true`.
- `non_idempotent_write`: `destructiveHint: true`.

Listings (`tools/list` and `server/discover`) carry `cacheScope: "private"`,
because the tool list depends on the user's authorized connections.

## Calls and outcomes

A call sends `io.superdurable.dex/idempotencyKey` in the request `_meta`. A
valid key is a non-empty UTF-8 string of at most 1024 bytes.

A key answers only the call that first used it. Its connector, operation,
connection, project, and argument digest must all match the record, and the
tool name decides the first three:

- **Current name.** A name the current tool set offers names that tool.
- **Vanished name.** A name that disappeared through a reload matches only a
  name the record's tool had, with or without connection qualification.
- **Unknown name.** When this build can no longer name the record's tool, the
  call answers `unknown`.

Any other call with the key is `rejected` ("the idempotency key was used for a
different call; nothing was sent"), and its state is left untouched.

**Expected-identity guard.** A request `_meta` may also carry
`io.superdurable.connectors/expectedConnector`, `expectedConnection`, and
`expectedOperation`, with the listing's identity values. When any is present,
every present key must equal the identity the call would run as. That is the
record's identity when the key already has state, otherwise the tool name's
current binding.

- A mismatch does not run. It answers JSON-RPC `-32602` with
  `error.data._meta` outcome `rejected` and a message naming the field. Existing
  state is never changed.
- When all three keys match a record, they identify its call even after the
  tool name was remapped to another connection.

Results use these shapes:

- **Success.** `content` is one text block with the result value as canonical
  JSON: sorted keys, no HTML escaping. `structuredContent` is the value when it
  is a JSON object.
- **Error results.** They set `isError: true`. `rejected` and `unknown` also
  set `io.superdurable.dex/outcome`. A read's `failed` has no outcome key,
  because Dex reserves other values. Dex records it `failed`.
- **Extra `_meta`.** Results carry `io.superdurable.connectors/branch` and,
  for a replay, `io.superdurable.connectors/replayed: true`.
- **Error text.** It names the operation and the connector `Failure` kind and
  message, which the connector contract keeps secret-safe. It never includes
  provider bodies or credentials.
- **`resultType`.** Under MCP 2026-07-28 every `tools/call` result, including
  error, replayed, and truncated results, carries `resultType: "complete"`, as
  that version requires. Older protocol versions get no `resultType`.

| Situation | `read` | `idempotent_write` | `non_idempotent_write` |
| --- | --- | --- | --- |
| Unknown tool, and the key has no state | JSON-RPC `-32602` with `error.data._meta` outcome `rejected` (HTTP 400 under MCP 2026-07-28, which the MCP Python SDK reads as the JSON-RPC error) | same | same |
| Arguments fail `inputSchema` | `rejected`, no state written | same | same |
| Arguments pass `inputSchema` but fail strict decoding | `rejected` | `rejected`, stored as the key's result | same |
| No key | runs, with inline retry | `rejected`, nothing sent | same |
| Malformed key | `rejected` | `rejected` | `rejected` |
| Key reused for another call (other tool, connection, project, or arguments) | runs (reads keep no state), `rejected` if the key holds a write record | `rejected`, nothing sent, state untouched | same |
| Expected-identity key mismatch | JSON-RPC `-32602` with outcome `rejected`, not run | same | same |
| Key's record names a tool this build cannot name, under a vanished name | n/a | `unknown`, state untouched | same |
| Running key whose connection now points to another target | n/a | no attempt: `rejected` if proven not sent, else `unknown` | finalized from its checkpoint (never runs again) |
| Attempt that did not succeed, after which the connection points to another target or is gone | `rejected`, no further attempt | no further attempt: `rejected` if proven not sent, else `unknown`; a listed `rejected` branch stays `rejected` unless an attempt may have applied | same |
| Completed record | n/a | replay | replay |
| Required branch | success | success | success |
| Listed `rejected` branch | `rejected` | `rejected`; `unknown` once an earlier attempt may have applied | `rejected` |
| Unlisted Query branch or Query `defect` | `failed` | n/a | n/a |
| `uncertain`, listed `unknown`, unlisted Mutation branch, Mutation `defect`, or a branch after the Fence may have sent | n/a | `unknown` | `unknown` |
| Connector Retry | inline retry; at the attempt cap `rejected`; budget spent: bare `-32603` | inline retry; at the cap or the window end `rejected` if proven not sent, else `unknown`; budget spent: bare `-32603`, record left running | no inline retry: `rejected` "not sent; the provider asked to retry later" if proven not sent, else `unknown` |
| Attempt ended without a branch (panic, unencodable result) | bare `-32603` | `unknown`; bare `-32603` if the operation never ran | `unknown`; `rejected` if the operation never ran |
| Call state unavailable before any send | runs without state | bare `-32603` | `rejected` when no record can exist, else `unknown` |
| Another attempt of the same key holds it until the client gives up | bare `-32603` | bare `-32603` | `unknown` |
| Record corrupt or of an unknown format | `unknown` | `unknown` | `unknown` |
| Server stopping | bare `-32603` | bare `-32603`; a completed key still replays | `rejected` when no record exists; a completed key replays; a running record is finalized |

"Proven not sent" requires two things:

- The checkpoint of the record's current attempt is a `singledispatch`
  `notSent` marker of this Call ID, with no provider ID. `canResend` follows a
  claim, so it proves nothing.
- The record's `possiblyApplied` flag is not set.

A non-idempotent write never ends in a bare `-32603`, because Dex records that
as `outcome_unknown` even when nothing was sent.

Results longer than `--max-result-bytes` are cut on a UTF-8 boundary and get
`\n[Dex] truncated, N bytes`. `structuredContent` is then omitted, and
`io.superdurable.connectors/truncatedBytes` holds the full size.

### Inline retry

Reads and idempotent writes retry a connector Retry inside one call.

- **Delays.** The delay after attempt `n` is the operation's
  `StepDefaults.ExecuteRetry`, or `DefaultConnectorStepExecuteRetryPolicy`:
  initial interval × coefficient^(n-1), capped at the maximum interval. A
  longer connector `RetryAfter` wins.
- **Attempt cap.** At most 3 attempts, or the policy's `MaximumAttempts` when
  it is lower. A write counts with the persisted `attempt`, so Dex re-issues
  never multiply attempts. A read counts per call.
- **Budget.** The first attempt of a call always runs. A later attempt starts
  only when its delay plus the attempt timeout fits in the rest of
  `--call-time-budget`. Otherwise the call returns a bare `-32603` and Dex
  re-issues it with the same key.
- **Retry window.** It is fixed at a write's first attempt: `firstAttemptAt`
  plus the policy's `TotalDuration` (1 hour when unset), never beyond the
  retention. No attempt starts at or after it.
  - The window is checked again after each retry delay.
  - Each attempt's deadline is the earlier of its timeout and the window end,
    so a slow attempt or credential resolution cannot send after the provider
    stops honoring its idempotency key.

Reads honor client cancellation. A write attempt is never cancelled by the
client. The operation's Execute timeout bounds it (60 seconds when unset), and
it runs to a stored result that a re-issue replays.

## Call state (`dex-connectors-mcp/call-state/v1`)

Records exist only for declared writes. The format never changes within v1.
The locks need a Unix system; elsewhere a keyed write never runs.

```text
<state-dir>/                       mode 0700; refused if a symlink or group/world-writable
  server.lock                      instance lock
  server.json                      discovery file, 0600
  calls/v1/
    FORMAT                         "dex-connectors-mcp/call-state/v1\n"; any other content refuses to start
    <k[0:2]>/<k>.json              one record per key, 0600
    <k[0:2]>/<k>.lock              per-key lock file, 0600
```

`k` is the lowercase hex SHA-256 of `dex-connectors-mcp/call-state/v1`, a NUL
byte, and the idempotency key. The raw key never becomes a file name and is
never logged.

```json
{
  "schemaVersion": "dex-connectors-mcp/call-state/v1",
  "stateKey": "<k>",
  "request": {"connectorId": "slack", "operationId": "postThreadReply", "connectionName": "work",
              "projectIdentity": "file:/Users/me/.dex/connectors/connections.json", "argumentsDigest": "<sha256 of canonical arguments>"},
  "requestDigest": "<sha256 of the canonical request object>",
  "effect": "non_idempotent_write",
  "callId": "<sdkgo Call ID>",
  "connectionTarget": "sha256:<digest of the connection's non-secret target>",
  "firstAttemptAt": "2026-11-20T10:00:00.123456789Z",
  "retryWindowEndsAt": "2026-11-20T11:00:00.123456789Z",
  "attempt": 1,
  "checkpoint": {"callId": "…", "attempt": 1, "state": "claimed"},
  "possiblyApplied": false,
  "status": "running",
  "result": null,
  "serverVersion": "0.1.3",
  "updatedAt": "2026-11-20T10:00:07Z"
}
```

- **Fields.**
  - `projectIdentity` is `file:` plus the real path of `connections.json`, or
    `project:<id>/<scope>` when hosted.
  - `argumentsDigest` treats absent or `null` arguments as `{}`.
  - `connectionTarget` is the `sdkgo` `projectconfig`
    `ConnectionTargetFingerprint` of the connection the first attempt was
    checked against: `"sha256:"` plus the hex SHA-256 of a canonical document
    whose schema is `connectors.dex.dev/connection-target/v1`. Credentials,
    their expiry, and the module path never enter it, so a credential refresh
    keeps it. Every attempt requires it while it resolves credentials. 0.1.0
    used another formula; see
    [Connection rebinding](#connection-rebinding).
  - `effect` is frozen at the first attempt. When a later build reclassifies
    the operation, the stricter effect applies.
  - `checkpoint` is the last heartbeat any attempt recorded. The next attempt
    receives it as its last heartbeat value.
  - `possiblyApplied` is set once any attempt may have reached the provider,
    and it is never cleared. That covers three cases:
    - an attempt ended `unknown` or without a branch after `Invoke`;
    - an attempt asked to retry without `notSent` proof;
    - an attempt's outcome was never observed, because a re-issue found the
      record running.

    While it is set, the call never ends `rejected`. A later listed rejection
    or `notSent` proof becomes `unknown`.
  - `result` is the exact MCP result returned.
- **Frozen identities.**
  - Each attempt runs with a `dex.Context` whose Flow ID is
    `dex-connectors-mcp` and whose Step execution ID is the idempotency key.
    The connector Call ID, and so any provider idempotency key, is therefore
    stable across processes and versions.
  - `Attempt()` is the persisted `attempt`, and `FirstAttemptAt()` is
    `firstAttemptAt`.
  - Until Dex Go SDK 1.41.0 ships `dex.NewStandaloneContext`,
    `internal/attemptcontext` implements the same contract.

Protocol for a keyed call:

1. Validate the key and take the in-process mutex, then `flock(LOCK_EX)` on
   `<k>.lock`. The wait is bounded by the request context. After locking, the
   open file must still be the file at the path, or the lock is retaken.
2. Take the current tool set, not the one in effect when the request arrived.
   Then read the record **before** resolving the tool name from that tool
   set.
   - An expected-identity key that differs from the record: refused, state
     untouched.
   - Another call (see [Calls and outcomes](#calls-and-outcomes)): `rejected`,
     state untouched.
   - `completed`: replay.
   - `running` non-idempotent: finalize from the checkpoint without another
     attempt.
   - `running`: set `possiblyApplied` unless the current attempt proved
     `notSent`.
   - `running` idempotent: finalize when the window passed, the cap was
     reached, the tool is no longer offered for that connection, or the
     connection's current target differs from `connectionTarget`. Otherwise
     increment `attempt`, write the record durably, then run. After an
     attempt that did not succeed, and again after each retry delay, the same
     tool and target checks run against the tool set as it is then.
3. No record: resolve the tool, validate the arguments, write a new record with
   `attempt: 1` durably, then run.
4. A durable write writes a temporary file in the same directory, syncs it,
   renames it, then syncs the directory. On darwin `os.File.Sync` issues
   `F_FULLFSYNC`.
5. During the attempt, every `RecordHeartbeat` stores the checkpoint durably
   before it returns, so the Fence's claim is durable before the send. Only
   the current attempt may store one.
6. Store a terminal result as `completed`, then respond. If that write fails,
   the answer is still sent. The record stays `running`, and a re-issue reads
   back or answers `unknown`.
7. Release `flock`, then the mutex.

Each crash point leaves this for the next call with the key:

| Crash point | Non-idempotent write | Idempotent write |
| --- | --- | --- |
| Before the first record write | runs as new | runs as new |
| After the record write, before the claim | `unknown` (nothing proves `notSent`) | next attempt; the Fence forces read-back |
| During the send, or after it before storing | `unknown` | next attempt reads back or resends with the provider key |
| After storing the result | replay | replay |

Retention:

- A pruner runs at start and then hourly.
- Under the key's lock, it removes records whose `updatedAt` is older than
  `--call-state-retention`, then lock files without a record after an hour.
  It deletes the record first and the lock file last.
- It also removes temporary files older than an hour.
- A key reused after retention is a new call.
- Stored write results can contain personal data. They stay in `0600` files.

### Connection rebinding

Dex Web keeps one record per connection name in `connections.json`, so a
connection of the same name can later point somewhere else. Each record stores
`connectionTarget`, and no attempt of an existing key ever runs against a
different target. A rebound key settles from its evidence and nothing runs:

- `rejected` ("the connection now points to a different target; nothing was
  sent") when the current attempt proved `notSent` and nothing possibly
  applied;
- `unknown` otherwise.

A new key after a rebind runs normally against the new target.

The target is `projectconfig.ConnectionConfiguration.ConnectionTargetFingerprint`
from `sdkgo` v0.36.0, the one definition of a connection's target; see
[Connection targets](../sdkgo/projectconfig/README.md#connection-targets). It
covers the connector, the connection name, the provider, `authMethodId`,
`authMethodIds` in order, Dex Web's `authorizationId`, `sharedConnectionId`,
and the settings. Credentials, their expiry, and the module path never enter
it.

**Before an attempt.** The check uses the tool set as it is when the call
holds the key's lock, never the one in effect when the request arrived. A
request that waited on the lock while the connection was rebound therefore
sees the new target. Before every attempt the call resolves the tool from a
current snapshot, compares that binding's target with `connectionTarget`, and
runs the attempt with the tool from the same snapshot:

- **New key.** The record's `connectionTarget` and the first attempt come from
  the snapshot taken after the lock.
- **Resumed key.** The binding comes from the snapshot taken after the lock.
- **Inline retry.** A new snapshot is taken after each retry delay.
- **Read.** Reads keep no state, but every attempt resolves the tool from a
  current snapshot and runs it from that snapshot. That covers the first
  attempt, each inline retry, and the read that runs when the call state is
  unavailable. If the tool disappeared, or its target changed, between attempts
  of one read, the read stops with `rejected` and makes no further attempt.

A tool that is gone, or a different target, settles a write's key as above.

**During an attempt.** A Dex Web rebind can land after the checks above and
before the connector resolves its credential. Every attempt therefore runs
under `projectconfig.ContextWithExpectedConnectionTarget` for its connection:
a write with the record's `connectionTarget`, and a read with the target it
checked when it started. `sdkgo` then reads each credential and the record's
target from one read of `connections.json`. When the target differs, or the
record is gone, it refuses the credential before any provider request,
including a token refresh. An attempt never pairs the checked settings with
another account's credential. A refresh of the same target keeps working,
because a refresh keeps `authorizationId` and the settings.

Connectors report that refusal through their usual credential-error path,
usually as Retry and sometimes as a branch, so it does not reach the server as
an error. After every attempt that did not succeed, and before any retry
delay, the server therefore compares the current target again. When the
target changed or the tool is gone, the call makes no further attempt:

- **Write.** The key settles from its evidence, as above. A Retry or `unknown`
  attempt answers `rejected` only with `notSent` proof. A listed `rejected`
  branch keeps its branch and adds "the connection now points to a different
  target". Once anything possibly applied, the answer is `unknown`. A target
  change never turns a possibly applied write into `rejected`.
- **Read.** The read stops with `rejected` ("its connection was removed or now
  points to a different target; nothing was changed").

An attempt that succeeded ran on the target it was checked against, and its
result stands. A write without a `singledispatch` Fence, such as one that reads
back and resends with a provider idempotency key, has no proof that a refused
attempt sent nothing, so it settles `unknown`.

**Hosted and shared connections.** Only a record of a Dex Web connections file
can be compared while a credential resolves. `sdkgo` refuses an expected
target with `ErrConnectionTargetUnverifiable` for hosted and S3 projects and
for shared entries (`sharedConnectionId`) that the credential broker serves.
Their records carry no authorization identity. For those connections:

- write tools are not offered, and the tool set carries a warning that names
  the connection;
- read tools are offered and run without an expected target. The checks
  before each attempt and after each attempt that did not succeed still apply;
- a running write key of such a connection settles because its tool is no
  longer offered: `rejected` if proven not sent, else `unknown`.

Detected, because the target changes:

- another authorization: Dex Web writes a new `authorizationId` for a new
  connection, every OAuth callback, and any change of the stored credentials.
  Reauthorizing to another account is detected even when the settings stay
  identical;
- any change to the connection's non-secret settings: endpoint or base URL,
  tenant, workspace, region, and account identifiers that a connector derives
  into its settings from OAuth or OIDC claims or setup commands;
- another provider, another authorization method (`authMethodId`), another
  selected method set (`authMethodIds`), or another shared connection
  (`sharedConnectionId`);
- a removed connection: its tool is no longer offered, so the key settles. A
  re-added connection is compared like any other;
- another connections file: `projectIdentity` differs, so the key's call is
  treated as a different call.

What remains:

- **Older Dex Web.** A record written by Dex Web before 1.43 has no
  `authorizationId`. Reauthorizing it with such a Dex Web to another account
  with identical settings is not detected. Dex Web 1.43 and later write the ID.
- **Unlocked saves.** Dex Web releases before superdurable/dex#1526 do not
  take the refresh lock when they save a reauthorized credential. The windows
  last milliseconds or less; see
  [Refresh across processes](../sdkgo/projectconfig/README.md#refresh-across-processes).
- **Hosted and shared connections** offer no write tools, as above.
- **Context.** The guard applies to credentials that a connector resolves
  through the attempt's context, which is the connector's normal call path. A
  credential cached across calls bypasses it.

The fingerprint is conservative. Reauthorizing the same account, and settings
re-saved with a new default field after a connector upgrade, also change it.
A running key then settles, usually `unknown`; repeat such a call with a new
key.

**Upgrading from 0.1.0.** dex-connectors-mcp 0.1.0 computed
`connectionTarget` with its own formula, without `authorizationId` and
`authMethodIds`. The call state stays `dex-connectors-mcp/call-state/v1`: the
field keeps its shape, `sha256:` and 64 hexadecimal digits, and only this
server reads it, to compare it for equality. A new `FORMAT` would instead make
the server refuse to start on every existing state directory. After the
upgrade:

- a completed record replays unchanged;
- a running record written by 0.1.0 never runs again. Its target reads as
  changed, so it settles `rejected` when its current attempt proved `notSent`
  and nothing possibly applied, and `unknown` otherwise. Repeat such a call
  with a new key;
- a new key records the new fingerprint.

## Security

- **Network.** The server listens on loopback only and has no token in this
  phase. Any local process can call the tools with the developer's
  connections.
- **Host check.** `Host` must be a loopback IP literal, `localhost`,
  `*.localhost`, or an `--allowed-host`. A non-loopback IP literal is refused.
- **Origin check.** A request with `Origin` is accepted only for an `http://`
  origin whose host passes the Host check. `http.CrossOriginProtection` checks
  `Sec-Fetch-Site` as a second layer. No CORS headers are sent.
- **Limits.** Request bodies are limited to 4 MiB. The header read timeout is
  10 seconds, and idle connections close after 90 seconds.
- **Credentials.** The connector SDK resolves credentials per call. The server
  never reads, logs, returns, or stores them.
- **Logs.** One line per call on stderr: tool, connector, operation,
  connection, effect, the first 12 characters of `k`, attempt, branch,
  outcome, duration, and whether it replayed. A JSON-RPC rejection logs
  outcome `rejected` with a `reason`: `unknownTool` or
  `expectedIdentityMismatch`. Logs never contain arguments, results, or
  failure messages.

## Development

The module requires released modules only and needs Go 1.25 or later:

```bash
cd connectorsmcp && GOWORK=off go test -race ./... && GOWORK=off go vet ./...
```

Never commit a `replace`.

- **Crash tests.** `internal/server/crash_test.go` re-executes the test binary
  as server processes. Each one kills itself with `SIGKILL` at each protocol
  point, or the test kills it while the fake provider holds the request. The
  tests assert at most one provider send, or one applied effect, per key.
- **Conventions fixture.** `internal/server/testdata/mcp_dex_conventions.json`
  is vendored from `superdurable/dex` at
  `18a5b6a905af42b4a297fb1efc7b11736c8654f0`, file
  `agents/conformance/mcp_dex_conventions.json`. The tests check a reference
  classifier against every case, then classify every tool definition and
  answer the server produces.
