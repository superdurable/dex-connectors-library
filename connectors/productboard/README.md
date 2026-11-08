# Productboard connector

The Go `listFeatures` Query calls Productboard REST API v2 `GET /entities` with `type[]=feature` and `fields[]=name`. It returns only feature IDs, names, and an opaque next-page cursor. Other entity fields are excluded. A blank `pageCursor` starts at the first page; pass `nextCursor` back to read the next page. HTTP 429, 408, 5xx, and transport failures retry. Definitive provider rejection and malformed pages select optional branches.

Create a Personal Access Token in [Productboard Workspace Settings > Integrations > Public APIs > Access Token](https://developer.productboard.com/reference/api-token). Enter it in Dex Web's secret `api_token` field. The public API endpoint, request timeout, and response limit have manifest defaults. See the [official List Entities reference](https://developer.productboard.com/reference/listentities) and [pagination guide](https://developer.productboard.com/reference/pagination).

The runnable Dex Flow is in [`go/examples/feature-discovery`](go/examples/feature-discovery/README.md). Its static connection name is `productboard`. This first version declares only the Go package; Python and TypeScript ports remain to be added.
