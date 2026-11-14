# Pgyer

The Go connector lists one page of mobile apps uploaded under the configured account using Pgyer's current [API 2.0 `listMy` endpoint](https://www.pgyer.com/doc/en/view/api_app). `ListUploadedApps` returns app/build IDs, name, platform, version, and upload time. It omits download links, account data, and install credentials. Page zero uses Pgyer's default first page; a positive page selects that page.

After real-name verification, sign in to [Pgyer](https://www.pgyer.com) and open the account menu → Account Center → [API Info](https://www.pgyer.com/account/api) to copy the API Key. Sub-accounts do not have their own key. The secret is sent only in the HTTPS form body as `_api_key`; the connector does not put it into Flow data, receipts, logs, URLs, or errors. Resetting the key invalidates it immediately. See the [official account guide](https://www.pgyer.com/doc/en/view/account_center) and [API overview](https://www.pgyer.com/doc/en/view/api).

Pgyer documents error code 1098 for an hourly API limit. Dex retries transient failures; the precise allowance depends on the account. The [runnable Dex Flow](go/examples/app-list/README.md) demonstrates connection setup and mock verification. Live Pgyer authorization and response have not been exercised.

The logo is sourced from [Pgyer's official website](https://assets3.pgyer.com/static-30000088/assets/img/logo_green_en.svg).
