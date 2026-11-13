# FOFA connector

This connector searches the public asset index of [FOFA](https://fofa.info/api), operated by [湖南华顺信安科技有限公司](https://fofa.info/about). `SearchDomainAssets` accepts one DNS domain, forms FOFA's `domain="..."` query, and returns at most five host, IP, and port records from the first page plus the total count. It uses `GET https://fofa.info/api/v1/search/all` with a secret Key in the query, as the official API requires.

The connector excludes the Key, raw URL, and provider error text from Results and failures. It disables redirects and bounds responses. FOFA search may consume F-points, so the retry policy permits only one additional attempt.

## Connection

Sign in at [FOFA](https://fofa.info), open the account avatar > personal center > API information, and copy the API Key into Dex Web Connections. Your plan must allow API queries. Leave timeout and maximum response size blank to use the displayed defaults.

Run the [example Flow](go/examples/search-domain-assets/) and start `FOFADomainAssetSearch` with `{"domain":"example.com"}`. The checked-in mock returns one asset. Live FOFA API behavior requires a user-provided Key and has not been verified here.
