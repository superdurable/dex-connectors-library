# Pinterest connector

The Go, Python, and TypeScript `listPins` Query reads one page of owned Pin identities and titles from the [Pinterest API v5](https://developers.pinterest.com/docs/api/v5/pins-list/). Go also offers `listBoards`, `getBoard`, `getPin`, `listBoardPins`, `listBoardSections`, and `listBoardSectionPins` using the existing `boards:read` and `pins:read` scopes. The runnable [Go](go/examples/pins/README.md) Flow reads Pins and boards with the static `pinterest` Dex Web connection; the [Python](python/examples/pins/README.md) and [TypeScript](typescript/examples/pins/README.md) Flows continue to read Pins. Pin and board writes require OAuth write scopes that existing connections do not have.

Register a Pinterest developer app from [My apps](https://developers.pinterest.com/apps/), obtain Trial access approval, register the exact redirect URI shown in Dex Web, and request `boards:read` and `pins:read`. Save its App ID and secret in Dex Web and authorize the user. Pinterest rotates access and refresh tokens; Dex holds them as secrets. Live approval, OAuth consent, token refresh, API reads, and Dex Web field rendering have not been verified in this checkout.

`listPins` accepts a page size from 1 through 250 (zero uses the provider's 25-Pin default) and an optional bookmark from a previous page. It returns only Pin IDs, board IDs, titles, and the next bookmark. A blank next bookmark means the last page. Pinterest returns HTTP 429 for rate limits; the connector retries 429, 408, and server errors. Applications should only list Pins they intend to persist in Dex.

`listBoards` accepts the same page size and bookmark rules. It returns board IDs, names, and the next bookmark. It projects away board descriptions and other profile data. The Go example invokes it after saving the Pin page; its Flow result remains the Pin page.

`getBoard` accepts a numeric `boardId` and returns its ID and name. `getPin` accepts a numeric `pinId` and returns its ID, board ID, and title. Each selects `notFound` when Pinterest returns HTTP 404; they retry rate limits and server errors. Both discard provider fields outside those projections.

`listBoardPins` and `listBoardSections` accept a numeric `boardId`, a page size from 1 through 250, and an optional bookmark. `listBoardSectionPins` also accepts a numeric `sectionId`. Zero page size uses Pinterest's 25-item default. The Pin page operations return the same small Pin projection as `listPins`; section pages return section IDs and names. All three return the next bookmark and retry rate limits and server errors.
