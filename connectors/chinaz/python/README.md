# Chinaz Python connector

The `dex-connector-chinaz` package implements the read-only `getSiteTDK` Query. It sends a site domain and the secret API Key in a multipart HTTPS POST body, then returns only public title, keywords, description, and IP. The package exports `GetSiteTDKInput`, `SiteTDK`, `new_get_site_tdk_step`, and a Dex project connection.

The [runnable Flow](examples/site_tdk/flow.py) uses the static `chinaz` connection and persists the filtered result. Its [integration test](examples/site_tdk/integration_test.py) exercises a 503 retry through a real Dex Worker and Client. The [conformance test](tests/test_conformance.py) replays 14 Go-recorded provider scenarios, including the multipart fields.

## Run with Dex Web

At the [Chinaz Site TDK API](https://api.chinaz.com/ApiDetails/SiteData), sign in and purchase or enable the Site TDK plan. In **User Center > My Data**, copy the Site TDK APPKEY into the secret `api_key` field of a Dex Web `chinaz` connection. Blank prevents requests. Leave base URL, timeout, and response size blank for their displayed defaults. Each query may consume plan allowance.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
uv run python -m examples.site_tdk.worker
```

In Dex Web start `ChinazSiteTDK` with a unique Flow ID and input `{"domain":"example.com"}`.

## Strict FDG 2.0 generation

```bash
dexcli visualize ./examples/site_tdk/flow.py --schema-version 2.0 --json --out /tmp/chinaz-python-fdg
```

The output must report `valid: true`. Tests use a fake provider; live Chinaz authorization and plan usage remain unverified.
