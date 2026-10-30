# Gmail connector for Python

`dex-connector-google-gmail` provides typed Gmail reads, sends, and replies for
Dex Python Flows. The package imports as `dex_connectors.google_gmail`.

## Install

After release, install `dex-connector-google-gmail==0.1.0`. The package requires
`dex-connectors-sdk>=0.1.0` and `dex-python-sdk>=1.28.0`; the latter provides
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

The Python connector uses the same manifest authorization guide and safe
defaults as the Go connector. Dex CLI's Python FDG 2.0 connector identity (R4)
must be released before Dex Web can discover the connection from a Python Flow.
Until that support is available, this package does not claim Dex Web Connections
verification. Do not put access tokens, refresh tokens, or service-account keys
in Flow input, results, Attributes, Streams, logs, or mock reports.

## Run the example

[`examples/thread_reply`](examples/thread_reply/main.py) reads one received
message, replies in its existing thread, and completes with the accepted reply
IDs. Its Worker loads the `gmail-inbox` connection from the Dex Web project
configuration. The entrypoint uses the public connection and Step APIs:

```python
provider = new_project_provider(project, CONNECTION_NAME)
connection = new_connection(provider, CONNECTION_NAME)
await run_thread_reply_worker(
    ThreadReplyFlow(connection),
    os.environ.get("DEX_BLOB_CACHE_DIRECTORY", "/tmp/dex-gmail-thread-reply-cache"),
    os.environ.get("DEX_SERVER_ADDRESS", "localhost:8801"),
)
```

Run `dexcli dev`, then from this `python/` directory run
`DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json python -m examples.thread_reply.main`.
The Flow type is `ThreadReplyFlow`; its input JSON contains `messageId` and
`replyText`. Use a real mailbox message ID and a reply body with no secrets.
The [integration test](examples/thread_reply/tests/test_integration.py) runs
the same Flow on a real Dex Worker and Client with a local fake Gmail HTTP
boundary and manifest mocks.

## Send confirmation

Gmail does not deduplicate messages by `Message-ID`. The connector derives a
stable run-specific RFC Message-ID from the Dex Call key, run ID, and Flow start
second. Its Python Mutation Steps use synchronous Execute durability so Dex
persists the single-dispatch heartbeat before a POST. On a numbered retry it
searches Sent Mail first. A prior claimed send can only be read back; a second
POST requires a persisted `notSent` or `canResend` marker. After a possibly
accepted POST loses its answer or returns 5xx, the connector polls Sent Mail
before returning Retry. A successful 2xx is already accepted even if its response is
malformed, oversized, or unreadable; a Sent Mail miss then returns `sent` with
an empty message ID. A search miss after a claimed POST remains Retry without
another dispatch; the Flow fails when its retry budget is exhausted. A definite
429 refusal and a transport failure proven to precede dispatch permit a later
send. The manifest limits sends and replies to five attempts within two minutes.

Live provider authorization and these Gmail behaviors remain unverified without
a user-controlled account: retention of the client-set Message-ID header,
`rfc822msgid` search matching, and Sent Mail indexing latency. No live provider
credentials are included in this package.
