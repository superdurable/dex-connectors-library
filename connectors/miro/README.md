# Miro connector

The Go package reads board identity, board items, and board members through Miro REST API v2. `listBoards` returns one offset page and accepts an optional name or description search. `listBoardItems` returns one cursor page of item IDs, types, and available text content; `getBoardItem` reads one item. `listBoardMembers` returns one offset page of member IDs and roles. `getBoard` retains its original board ID and name result. An inaccessible resource selects `notFound`; 429, transport failures, and 5xx responses retry.

Miro requires the `boards:read` scope. In [Your apps](https://miro.com/app/settings/user-profile/apps), create or select a Developer team app, add that permission, and use **Install app and get OAuth token** to authorize it for a team. Enter the resulting opaque token in Dex Web's secret `access_token` field. A static token connection needs rotation if the token expires. See Miro's [Quickstart](https://developers.miro.com/docs/rest-api-build-your-first-hello-world-app) and [Get specific board reference](https://developers.miro.com/reference/get-specific-board-1).

The connection guide provisions `boards:read`. Miro requires `boards:write` for board and item creation, updates, and deletion; those operations need a token with additional permission and are outside this connection's read-only contract. The runnable Dex Flows are in [`go/examples/board-inspection`](go/examples/board-inspection/README.md). Their static connection name is `miro`. The Go release is `v0.2.0`; Python and TypeScript remain at `v0.1.0` with `getBoard` only. Go-recorded conformance scenarios cover every Go operation, and each language replays its declared operations.

## Go operations

| Operation | Input | Result |
| --- | --- | --- |
| `getBoard` | `boardId` | Board ID and name |
| `listBoards` | Optional `query`, zero-based `offset`, page `limit` (1–50) | Board IDs and names, offset, total |
| `listBoardItems` | `boardId`, optional prior `cursor`, page `limit` (10–50) | Item IDs, types, content, next cursor |
| `getBoardItem` | `boardId`, `itemId` | Item ID, type, content |
| `listBoardMembers` | `boardId`, zero-based `offset`, page `limit` (1–50) | Member IDs and roles, offset, total |

The endpoints and permission requirements follow Miro's [boards](https://developers.miro.com/reference/get-boards-1), [items](https://developers.miro.com/docs/rest-api-reference-guide), and [board members](https://developers.miro.com/reference/get-board-members-1) references. Live Miro authorization and retrieval remain unverified.
