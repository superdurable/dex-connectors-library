# Help Scout Python connector

`dex-connector-helpscout` provides Dex-native Queries for Help Scout Inbox conversation search, conversation detail, and customer lookup, plus Mutations for replies, notes, and conversation updates. The Python package implements the five listed operations. The `conversationEvent` webhook Trigger remains Go-only.

## Install

```sh
uv add dex-connector-helpscout==0.1.0
```

This package requires `dex-connectors-sdk==0.5.0` and `dex-python-sdk>=1.28.0`.

## Configure a connection

In Dex Web **Connections**, create a `helpscout` connection named `helpscout`. Sign in at [Help Scout](https://secure.helpscout.net/), open your avatar, then **Your Profile > My Apps**, and create an app. Copy its App ID into `app_id` and its secret App Secret into `app_secret`. Leave `access_token` blank: the generated client credentials driver requests and renews it using Help Scout's token endpoint. Help Scout apps have no scopes. The Free plan has no API access. The app acts with its owner's inbox permissions.

The optional `webhook_secret` is for Go's webhook Trigger and is unused by this Python operations package. Leave `maxResponseBytes` and `webhookMaxBodyBytes` blank for manifest defaults. `maxResponseBytes` limits successful API response bodies in bytes; increase it only for large conversation and thread pages. `webhookMaxBodyBytes` applies to the Go webhook endpoint and does not change Python operation requests.

`new_project_connection(project, "helpscout")` loads the manifest settings and generated private or shared credential decoders. It resolves credentials at call time, uses the generated refresh driver, and retries once after a provider 401 if the host can refresh. The connection and secrets stay outside Flow input, results, and Attributes. Close the connection after Worker shutdown.

## Run the example Flow

[`examples/conversation_search/worker.py`](examples/conversation_search/worker.py) is the Worker entrypoint. It loads the named Dex Web connection and registers `HelpScoutConversationSearch`. From this package directory:

```sh
uv run python -m examples.conversation_search.worker
```

Start the Flow with [`input.example.json`](examples/conversation_search/input.example.json). It returns one page and stores it in Flow persistence. The example wires only `searched`, so optional failure branches fail the Flow. Its static Step annotations include the group and explanation for Flow Definition Graph generation.

## Operation contract

`searchConversations` combines inbox, status, tag, customer email, modification time, and page filters. `getConversation` reads one conversation and the newest thread page, and selects `merged` for Help Scout's redirect to another conversation. `findCustomerByEmail` returns all matches on its first page, up to 50; no match selects `notFound`.

`replyToConversation` uses Dex's durable single-dispatch checkpoint. It sends a reply or internal note at most once after an ambiguous outcome and selects `uncertain` when the provider may have accepted it without a confirmable thread ID. A confirmed ID is read back on retry. `updateConversation` compares the current state, sends absolute status, assignee, and tag writes only when needed, and reads the final state back. Repeat attempts converge on the requested values.

Every operation uses the generated Step factory and its stable branch IDs. Only the required happy-path branch should be wired by default. The provider can return new conversation status strings, which results preserve unchanged. Treat provider text and HTML as untrusted before rendering.

## Verify locally

```sh
uv sync
uv run pytest tests -q
uv run ruff check src tests examples integration
uv run mypy src/dex_connectors/helpscout
```

The package suite replays the Go-recorded provider requests and outcomes and the generated OAuth refresh agreement. For the real Dex Worker and Client test, start `dexcli dev` on unused ports, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address, then run `uv run pytest integration/conversation_search.py -q`. The provider stand-ins use an OS-selected transport address; no live Help Scout authorization was available during this port, so real provider token minting and Inbox calls remain unverified.
