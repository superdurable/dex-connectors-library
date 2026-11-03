# Front connector for Python

`dex-connector-front==0.1.0` implements all five Front operations as Dex Python
Queries and Mutations: `searchConversations`, `getConversation`,
`replyToConversation`, `updateConversation`, and `findContactByEmail`. It has
no Python Trigger. Import from `dex_connectors.front`; the generated models,
Step factories, connection binding, mock outcomes, and `dex-connector.json`
come from [`connector.yaml`](../connector.yaml).

The connector requires Python 3.11 or newer, Dex Python SDK 1.28.0 or newer,
and the pinned `dex-connectors-sdk` release in `pyproject.toml`.

## Connection

The runnable example uses the static name `front-support`. Create an API token
at <https://app.frontapp.com/> under **Settings > Developers > API Tokens**.
A company admin chooses **Create API token**, grants contact read access for
this example, then copies the token into the secret `api_token` field in Dex Web
**Connections**. The token has three dot-separated JWT segments and is sent
only as an Authorization bearer token. The `maxResponseBytes` setting limits
one complete response in bytes; leave it blank to retain the displayed safety
limit, and raise it only when a valid large response selects `invalidResponse`.
The [Front connector guide](../README.md#authorization) details permissions
for every operation and the inbox, teammate, and tag pickers.

The runnable [`main.py`](examples/contact_lookup/main.py) loads project
configuration and resolves private credentials at call time through
`new_project_connection(project, "front-support")`. Secrets stay out of Flow
input, Results, Attributes, receipts, logs, and generated manifests.

## Operation outcomes

The generated operation-specific factories accept one required happy-path
branch. Optional branches fail the Flow when left unwired. Searches select
`searched`; conversation reads and contact lookups select `found`; replies
select `replied`; updates select `updated`. Reads and updates retry safe
transient failures. A reply or internal comment uses the SDK single-dispatch
fence because Front has no provider idempotency key. Ambiguous delivery
selects `uncertain`; reconcile it in Front before making a new request. An
update reads back its requested values and can repeat absolute writes.

`frontmock.Mock` and its generated named scenarios can exercise branch routing
without credentials. The 50 Go-recorded provider scenarios in
[`conformance/connectors/front`](../../../conformance/connectors/front/scenarios.json)
exercise exact HTTP requests, response decoding, branches, retry decisions,
and the production SDK output codec. They supplement the real Dex Worker and
Client [example integration test](examples/contact_lookup/integration_test.py).

## Run the example

See the [contact lookup Flow](examples/contact_lookup/README.md). From this
package directory, install and run its Worker after configuring Dex Web:

```sh
uv sync --locked
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.contact_lookup.main
```

For local verification with `dexcli dev`, use `uv run pytest` for the package
suite and set `DEX_FLOW_SERVICE_ADDRESS` to the active local Dex gRPC address
when running `uv run pytest examples/contact_lookup/integration_test.py`.
The local integration test uses a free-port Front stand-in; Front account
setup, API permissions, and live provider behavior remain unverified.
