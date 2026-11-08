# Lucid connector

The Go `listDocuments` Query calls Lucid REST API v1 `POST /documents/search` with `excludeTrashed: true`. Although the endpoint uses POST, it only searches documents. The connector returns one page of document IDs and titles plus an opaque next-page token from Lucid's `Link` header. It excludes document content, owners, URLs, and custom attributes. HTTP 429, 408, 5xx, and transport failures retry.

Create a personal API key in the [Lucid Developer Portal > API Keys](https://lucid.app/developer) with the `DocumentReadonly` grant. Enter it in Dex Web's secret `api_key` field. This is intended for personal or internal use; Lucid recommends OAuth clients for published apps. See [Search Documents](https://developer.lucid.co/reference/searchdocuments), [Using the key](https://developer.lucid.co/reference/using-the-key), and [pagination](https://developer.lucid.co/reference/reference-rest#pagination).

The runnable Dex Flow is in [`go/examples/document-discovery`](go/examples/document-discovery/README.md). Its static connection name is `lucid`. Go, Python, and TypeScript packages each declare their first `v0.1.0` release and share Go-recorded conformance scenarios.
