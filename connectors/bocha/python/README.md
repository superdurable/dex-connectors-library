# Bocha Python connector

The `dex-connector-bocha` package implements the read-only `searchWeb` Query. It sends the API Key only in the authorization header and returns at most five public titles, HTTPS URLs, and snippets. The package exports `SearchWebInput`, `SearchWebOutput`, `WebResult`, `new_search_web_step`, and a Dex project connection.

The [runnable Flow](examples/web_search/flow.py) uses the static `bocha` connection and persists the filtered search result. Its [integration test](examples/web_search/integration_test.py) exercises one 503 retry through a real Dex Worker and Client. The [conformance test](tests/test_conformance.py) replays 12 Go-recorded provider scenarios.

## Run with Dex Web

Create a `bocha` connection in Dex Web. At the [Bocha open platform](https://open.bochaai.com/api-keys), sign in and open **账户管理 > API KEY管理 > 创建 API KEY**. Copy the one-time secret into the API Key field; blank prevents requests. Leave timeout and maximum response size blank for their displayed defaults. Searches may consume provider credit.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
uv run python -m examples.web_search.worker
```

In Dex Web start `BochaWebSearch` with a unique Flow ID and input `{"query":"博查 搜索 API"}`.

## Strict FDG 2.0 generation

```bash
dexcli visualize ./examples/web_search/flow.py --schema-version 2.0 --json --out /tmp/bocha-python-fdg
```

The output must report `valid: true`. The tests use a fake provider; live Bocha authorization, billing, and search responses remain unverified.
