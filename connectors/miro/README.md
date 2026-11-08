# Miro connector

The Go `getBoard` Query calls Miro REST API v2 `GET /boards/{board_id}` and returns only the board ID and name. Descriptions, owner, members, and sharing policy are excluded. An inaccessible or missing board selects `notFound`; 429, transport failures, and 5xx responses retry.

Miro requires the `boards:read` scope. In [Your apps](https://miro.com/app/settings/user-profile/apps), create or select a Developer team app, add that permission, and use **Install app and get OAuth token** to authorize it for a team. Enter the resulting opaque token in Dex Web's secret `access_token` field. A static token connection needs rotation if the token expires. See Miro's [Quickstart](https://developers.miro.com/docs/rest-api-build-your-first-hello-world-app) and [Get specific board reference](https://developers.miro.com/reference/get-specific-board-1).

The runnable Dex Flow is in [`go/examples/board-inspection`](go/examples/board-inspection/README.md). Its static connection name is `miro`. Go, Python, and TypeScript packages each declare their first `v0.1.0` release and share Go-recorded conformance scenarios.
