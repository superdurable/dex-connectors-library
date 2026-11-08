# Pinterest connector

The Go, Python, and TypeScript `listPins` Query reads one page of owned Pin identities and titles from the [Pinterest API v5](https://developers.pinterest.com/docs/api/v5/pins-list/). The runnable [Go](go/examples/pins/README.md), [Python](python/examples/pins/README.md), and [TypeScript](typescript/examples/pins/README.md) Flows use the static `pinterest` Dex Web connection. Pin creation and new-Pin delivery remain in the backlog.

Register a Pinterest developer app from [My apps](https://developers.pinterest.com/apps/), obtain Trial access approval, register the exact redirect URI shown in Dex Web, and request `boards:read` and `pins:read`. Save its App ID and secret in Dex Web and authorize the user. Pinterest rotates access and refresh tokens; Dex holds them as secrets. Live approval, OAuth consent, token refresh, API reads, and Dex Web field rendering have not been verified in this checkout.

`listPins` accepts a page size from 1 through 250 (zero uses the provider's 25-Pin default) and an optional bookmark from a previous page. It returns only Pin IDs, board IDs, titles, and the next bookmark. A blank next bookmark means the last page. Pinterest returns HTTP 429 for rate limits; the connector retries 429, 408, and server errors. Applications should only list Pins they intend to persist in Dex.
