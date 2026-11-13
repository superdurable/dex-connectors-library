# WeCom connector

The Go connector sends messages through WeCom (WeChat Work, 企业微信), Tencent's
workplace messaging service. It offers two operations, each with its own
connection method:

| Operation | Method | Sends to |
| --- | --- | --- |
| `sendGroupRobotMessage` | `group-robot` | the one internal group chat that owns a group robot (群机器人, now called 消息推送) webhook |
| `sendApplicationMessage` | `application` | members, departments, or tags in a self-built application's (自建应用) visible range |

Personal WeChat accounts have no official API. Reaching WeChat users through
WeCom external contacts (客户联系) or WeChat Customer Service (微信客服), contacts
reads, approvals, and callback Triggers are outside this release.

```bash
go get github.com/superdurable/dex-connectors-library/connectors/tencent/wecom/go@v0.1.0
```

## Connections

A connection selects one method, so an application that uses both operations
declares two connections, such as `wecom-robot` and `wecom-app`. Each operation
selects `defect` without contacting WeCom when its connection uses the other
method.

| Setting | Default | Meaning |
| --- | --- | --- |
| `baseUrl` | `https://qyapi.weixin.qq.com` | WeCom server API origin; override only for a test server or an approved proxy |
| `timeout` | `10s` | bound on one WeCom HTTP request |
| `maxResponseBytes` | `262144` | bound on one WeCom JSON response |

### Group robot

In the WeCom app, open an internal group chat, then chat settings > 消息推送
(Message Push, formerly 群机器人) > 添加 (Add). Copy only the value after `key=`
in the webhook address `https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=...`
into the secret `webhook_key` credential. Anyone holding the key can post to
the group; remove the robot and add a new one to revoke it. WeCom accepts at
most 20 robot messages per minute
([group robot guide](https://developer.work.weixin.qq.com/document/path/91770)).

### Self-built application

In the [admin console](https://work.weixin.qq.com/wework_admin/frame#apps),
open 应用管理 (App Management) > 自建 (Self-built) and create or select an
application. Set its 可见范围 (Visible Range) to every recipient. Store:

- `corp_id`: 我的企业 (My Company) > 企业信息 (Company Info) > 企业ID;
- `agent_id`: the application's AgentId, a positive decimal integer;
- `corp_secret`: the application's Secret > 查看 (View), delivered to an
  administrator's WeCom app. It is a secret credential.

WeCom can accept a self-built application's calls only from the trusted IP
addresses under 开发者接口 (Developer API) > 企业可信IP (Trusted Company IP).
Add the public egress IP of every Dex Worker. If the console asks for a 可信域名
(trusted domain) or a 接收消息 (Receive Messages) server URL first, configure
one of them. Calls from other IPs fail with errcode 60020, which the connector
reports as `providerRejected`.

## Operations

### `sendGroupRobotMessage`

Posts one `text` (at most 2048 UTF-8 bytes, optional `mentionedUserIds` and
`mentionedMobileNumbers`, where `@all` mentions everyone) or `markdown` (at
most 4096 UTF-8 bytes) message. The result records the message type and
content size, never the content.

WeCom robot posts accept no idempotency key, return no message ID, and cannot be
read back. The operation therefore uses sync Execute durability and the
`singledispatch` fence: once a post may have reached WeCom, no later attempt of
the same Step posts again.

| Outcome | Selected when |
| --- | --- |
| `sent` | errcode 0 |
| Retry | errcode -1 (system busy), 45009 (frequency limit, retried after one minute), or 45033 (concurrency limit); HTTP 429 or 503; or a DNS, dial, or proxy CONNECT failure of net/http's own Transport before a connection was made |
| `providerRejected` | any other errcode, such as 93000 (invalid webhook or robot removed), 93004 (robot disabled), or 45002 (content too large), or another HTTP 4xx |
| `uncertain` | HTTP 5xx other than 503, HTTP 408, an interrupted, malformed, or oversized response, any other transport failure, any transport failure of a custom `WithHTTPClient` RoundTripper, or a later attempt after such an outcome or a lost Worker |
| `defect` | invalid input, a connection that is not `group-robot`, or a malformed `webhook_key`; nothing was posted |

Wire `uncertain` only when the application can ask a person to check the
group chat; unwired, it fails the Flow so the outcome stays visible.

### `sendApplicationMessage`

Sends one `text` or `markdown` message of at most 2048 UTF-8 bytes to
`toAllMembers`, or to at most 1000 `toUserIds`, 100 `toDepartmentIds`, and 100
`toTagIds`. `isConfidential` makes a text message unforwardable and
watermarked. The result holds WeCom's `messageId` (msgid) and the
`invalidUserIds`, `invalidDepartmentIds`, `invalidTagIds`, and
`unlicensedUserIds` that WeCom skipped; WeCom still delivers to the remaining
recipients.

Every request sets `enable_duplicate_check` to 1 and `duplicate_check_interval`
to 14400 seconds, the documented four-hour maximum
([message/send](https://developer.work.weixin.qq.com/document/path/90236)).
Each attempt sends byte-identical JSON, so WeCom suppresses the repeat of an
accepted message, and an unconfirmed attempt retries. Two consequences follow:

- WeCom also suppresses an identical message (same recipients, type, and
  content) that another Flow sends within four hours. Include a distinguishing
  detail, such as an incident ID or time, when repeated identical
  notifications must all arrive.
- The default one-hour Execute retry window stays inside the four-hour
  interval. Under an `ExecuteRetry` override that runs longer, a retry that
  starts 3 hours 50 minutes or more after the Step's first attempt
  (`DuplicateCheckResendCutoff`, measured from Dex's `FirstAttemptAt`) selects
  `uncertain` instead of sending, because WeCom would no longer suppress a
  repeat. The ten-minute margin covers request time and clock skew.

| Outcome | Selected when |
| --- | --- |
| `sent` | errcode 0, including partial delivery |
| Retry | errcode -1, 45009, or 45033; errcode 40014, 41001, or 42001 after discarding the cached token; HTTP 429, 408, or 5xx; a transport failure; or an interrupted, malformed, or oversized response, while within `DuplicateCheckResendCutoff` |
| `providerRejected` | a refused token request, such as 40001 (invalid secret), 40013 (invalid corp ID), or 60020 (untrusted IP), or a refused send, such as 81013 (no valid recipient), 40056 (invalid AgentId), or 301002 (token for another application) |
| `uncertain` | a retry at or after `DuplicateCheckResendCutoff`; an earlier attempt may have delivered the message |
| `defect` | invalid input, a connection that is not `application`, or malformed application credentials; nothing was sent |

The operation never retries 40001 or 40013, because WeCom can block an IP that
repeats a wrong secret.

### Access tokens

`gettoken` exchanges the corp ID and secret for an access token valid for 7200
seconds, and WeCom rate-limits callers that do not cache it
([gettoken](https://developer.work.weixin.qq.com/document/path/91039)). The
`Client` keeps one token per corp ID and SHA-256 fingerprint of the secret in
memory, so connections that share a `Client` never evict each other's token,
and renews each five minutes before expiry. Concurrent Steps share one exchange
per key. A rotated secret never reuses the previous token, and a token that
WeCom revokes early is discarded before the retry. Cache entries are never
removed, so their number is bounded by the distinct credentials one `Client`
serves during its lifetime.

The token is not a stored credential. `gettoken` is a query-string GET, not an
OAuth 2.0 client-credentials grant, so the manifest declares no token grant and
a host cannot mint the token for a shared connection. Each Worker process
exchanges its own token, once per two hours per application.

## Security

Webhook keys, application secrets, and access tokens travel in WeCom query
strings. The connector never copies a request URL, transport error, or WeCom
`errmsg` into a failure, result, receipt, or log; failures name only the
errcode. Message content stays in Flow input and is not repeated in results.

## Runnable examples

- [Group robot message](go/examples/group-robot-message/README.md) posts one
  message through the `wecom-robot` connection.
- [Application message](go/examples/application-message/README.md) sends one
  message through the `wecom-app` connection.

## Verification

From `connectors/tencent/wecom/go`:

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go test -p 1 -tags=integration ./... -count=1
```

The integration tests need a running `dexcli dev -durability
process-crash-safe` and use `DEX_FLOW_SERVICE_ADDRESS` when it is not
`127.0.0.1:8801`. They run both examples on real Dex against the generated
`wecommock` connection and against local fake WeCom servers, including a robot
post retried after errcode -1, a robot post that is never repeated after a
dropped connection, and an application send retried with identical bytes and a
cached token, plus a robot post accepted by WeCom while its Worker is
force-stopped: the attempt on the replacement Worker reads the claim checkpoint
and selects `uncertain` without posting again. With the release artifact loaded through `dexcli dev
--connector-release-override`, Dex Web Connectors rendered both senders as
**Local override**, with their guides, every field, and the displayed defaults;
no credential was saved.

Not verified against WeCom itself: no live robot post or application send has
run, and these provider behaviors remain unconfirmed:

- the response WeCom returns when duplicate checking suppresses a repeat; the
  connector assumes errcode 0 and would report any other errcode as
  `providerRejected` although the first send was delivered;
- the HTTP status and body of errcodes -1, 45009, and 45033 on the robot
  webhook, assumed to be HTTP 200 like other WeCom errors;
- which applications require trusted IPs, whether 可信域名 must come first, and
  the console paths named above, including 消息推送 in current WeCom clients;
- whether a new `gettoken` call invalidates the previous token;
- whether WeCom measures `duplicate_check_interval` from the first accepted
  send; the cutoff assumes it does and that the Worker clock is within ten
  minutes of Dex's `FirstAttemptAt`.
