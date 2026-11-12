# Productboard connector

The Go `listFeatures` Query calls Productboard REST API v2 `GET /entities` with `type[]=feature` and `fields[]=name`. It returns only feature IDs, names, and an opaque next-page cursor. Other entity fields are excluded. Omitting `pageCursor` or leaving it blank starts at the first page; pass `nextCursor` back to read the next page. The connector accepts a `links.next` URL only at `https://api.productboard.com/v2/entities` or at `/entities` below the configured `baseUrl`, such as a proxy. HTTP 429, 408, 5xx, and transport failures retry. Definitive provider rejection and malformed pages select optional branches.

Create a Personal Access Token in [Productboard Workspace Settings > Integrations > Public APIs > Access Token](https://developer.productboard.com/reference/api-token). Enter it in Dex Web's secret `api_token` field. The public API endpoint, request timeout, and response limit have manifest defaults. See the [official List Entities reference](https://developer.productboard.com/reference/listentities) and [pagination guide](https://developer.productboard.com/reference/pagination).

The runnable Dex Flow is in [`go/examples/feature-discovery`](go/examples/feature-discovery/README.md). Its static connection name is `productboard`. Go, Python, and TypeScript packages each declare `v0.2.0` and share Go-recorded conformance scenarios.
