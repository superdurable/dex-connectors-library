# Caoliao Python connector

The `dex-connector-caoliao` package implements the read-only `listQRCodes` Query. It sends the OpenAPI Key in an Authorization header and returns one page of QR-code names, coding identifiers, a next-page token, and the total count. It excludes scanned records and other provider data.

The [runnable Flow](examples/qrcode_directory/flow.py) uses the static `caoliao` connection and persists the page. Its [integration test](examples/qrcode_directory/integration_test.py) exercises a 503 retry through a real Dex Worker and Client. The [conformance test](tests/test_conformance.py) replays 14 Go-recorded scenarios.

## Run with Dex Web

At [Caoliao's Data API page](https://user.cli.im/), sign in and open **数据API > 添加配置 > OpenAPI > 开通**. Name and save the configuration, then copy its OpenAPI Key from the list into the secret `api_key` field of a Dex Web `caoliao` connection. Blank prevents requests. The [official authorization guide](https://cli.im/open-api/openapi/v2/auth.html) documents this path and Bearer format. Leave base URL, timeout, and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
uv run python -m examples.qrcode_directory.worker
```

In Dex Web start `CaoliaoQRCodeCatalog` with a unique Flow ID and input `{"pageSize":2,"pageToken":""}`. Pass a previous `nextPageToken` to request another page.

## Strict FDG 2.0 generation

```bash
dexcli visualize ./examples/qrcode_directory/flow.py --schema-version 2.0 --json --out /tmp/caoliao-python-fdg
```

The output must report `valid: true`. The tests use a fake provider; live Caoliao authorization and organization visibility remain unverified.
