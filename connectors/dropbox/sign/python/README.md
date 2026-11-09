# Dropbox Sign Python connector

This package implements `getSignatureRequest` for Dex Python Flows. It calls Dropbox Sign API v3 and returns only a signature request's ID, title, and completion, declined, and error flags. Go-recorded conformance scenarios verify exact requests, branches, retries, and outputs.

Create or reveal an API key under [Dropbox Sign API Settings](https://app.hellosign.com/home/settings/api) > **API Keys** and save it in Dex Web's secret `api_key` field. The connector sends it as the HTTP Basic username with an empty password. Leave `baseUrl` blank for the public API endpoint. See the [connector README](../README.md) and [runnable example](examples/signature_request_inspection/README.md). Live provider authorization remains unverified.
