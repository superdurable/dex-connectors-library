# NewsAPI Python connector

`dex-connector-newsapi` provides the `dex_connectors.newsapi` Query operation for
NewsAPI's [everything endpoint](https://newsapi.org/docs/endpoints/everything).
It searches one page of articles and returns typed article data. The connector
ID and connection provider are both `newsapi`.

## Install

```sh
uv add dex-connector-newsapi==0.1.0
```

The package requires `dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`.
Release the Connector SDK before this package.

## Configure a connection

In Dex Web **Connections**, create a `newsapi` connection named `newsapi`.
Open [NewsAPI registration](https://newsapi.org/register), create an account,
then sign in at [Your account](https://newsapi.org/account). Copy the 32
lowercase hexadecimal characters shown under **Your API key** into the secret
`api_key` field. The key is sent only in `X-Api-Key`; it is never placed in a
URL or Flow state.

Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for the manifest
defaults. Override `baseUrl` only for a compatible gateway or loopback test
server; it must be HTTPS unless it is loopback. `timeout` is a positive Go
duration, such as `15s` or `1m`. `maxResponseBytes` is a positive integer byte
limit. An oversized response selects `invalidResponse`.

`new_project_connection` loads these ordinary settings once from a
`LoadedProject` and resolves the current private `api_key` for each operation
attempt. The connection and credentials must stay outside Flow input and
persisted state. For custom setup, `new_connection(client, reference)` binds
an explicit `ConnectionRef`.

## Run the example Flow

[`examples/company_news/worker.py`](examples/company_news/worker.py) is the
Worker entrypoint. It loads the Dex Web project connection named `newsapi`,
registers the checked-in [`CompanyNewsFlow`](examples/company_news/flow.py),
and serves through an `AsyncWorker`. From the runnable Worker:

```python
connection = new_project_connection(project, CONNECTION_NAME)
registry = dex.Registry([CompanyNewsFlow(connection)], allow_async_handlers=True)
```

With Dex running and the connection saved in Dex Web, run from this package
directory:

```sh
uv run python -m examples.company_news.worker
```

Start the `NewsAPICompanyNews` Flow with input such as
`{"company":"Meridian","days":10,"maxArticles":20,"language":"en"}`.
Its successful output contains `status: "found"`, the fixed publication window,
and one page of typed articles. The Flow wires only `searched`; any optional
error branch fails the Flow. The checked-in Step directives and static factory
annotations supply Dex Flow Definition Graph 2.0 group and explanation metadata.
The generated `dex-connector.json` ships inside the Python package. Dex CLI
v1.28.0 still reports `unknown_step_target` for the example's literal
`StepRef` transition. Strict FDG for this local-to-connector route needs Dex
CLI release after 1.28.0 (tracked in
[dex-connectors-library issue #362](https://github.com/superdurable/dex-connectors-library/issues/362));
the Flow runs successfully in Dex.
The Go example's custom Dex Web summary and display RPCs are a follow-up for
this Python example.

## Operation contract

`searchArticles` accepts `SearchArticlesInput`. Set at least one of `query`,
`sources`, or `domains`. The connector sends optional fields only when set.
The required `searched` branch returns `ArticlePage`, even when the page is
empty. `providerRejected`, `invalidResponse`, and `defect` are optional
branches. An unwired optional branch fails the Flow when selected.
`new_search_articles_step` takes a static `connection_name`, a concrete
`step_input_type`, and one keyword per branch; it rejects a name that differs
from the bound connection reference. Use an inline `annotations` object with
`groupId`, `groupLabel`, and `explanation` for Dex Flow Definition Graph
analysis; a `StepAnnotations` instance is also accepted for programmatic use.

The Query makes one read-only HTTP request per Execute attempt. HTTP 429, 408,
and 5xx responses retry when safe; a known exhausted API key selects
`providerRejected` with `QUOTA_EXHAUSTED`. Provider error messages are never
persisted. Redirects are not followed. The Execute default is async, with a
30-second method timeout and up to one hour of retries.

Article text is untrusted provider content. Escape titles, descriptions, and
content before rendering HTML. A NewsAPI `[Removed]` article is returned
unchanged. Nullable article fields remain `None` when the provider sends null.

## Local tests

From the repository root, after installing the core SDK and Dex:

```sh
uv run --project connectors/newsapi/python pytest connectors/newsapi/python/tests
DEX_FLOW_SERVICE_ADDRESS=localhost:8801 uv run --project connectors/newsapi/python pytest connectors/newsapi/python/integration/company_news.py
```

The integration test requires a running `dexcli dev` server and exercises the
example through a real Dex Worker with the connector mock. Provider
authorization and the hosted NewsAPI API have not been exercised locally; the
provider tests use a credential-safe fake HTTP transport.
