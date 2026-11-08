# Dropbox Sign connector

The Go `getSignatureRequest` Query calls [`GET /signature_request/{signature_request_id}`](https://developers.hellosign.com/api/signature-request/get) and returns the request ID, title, and `is_complete`, `is_declined`, and `has_error` flags. Signer details, document contents, custom fields, and provider metadata are excluded. Missing requests select `notFound`; rate limits, transport failures, and 5xx responses retry.

[Dropbox Sign API authentication](https://developers.hellosign.com/api/api-reference-authentication) accepts an API key as the HTTP Basic username with an empty password. In [API Settings](https://app.hellosign.com/home/settings/api), open **API Keys**, create or reveal a key, and save its complete opaque value in Dex Web's secret `api_key` field. Revoke or rotate it from the same page. The key is resolved at call time and never belongs in Flow input.

The runnable Dex Flow is in [`go/examples/signature-request-inspection`](go/examples/signature-request-inspection/README.md). Its static connection name is `dropbox-sign`. The first Go package version is `v0.1.0`. Python and TypeScript packages are pending.
