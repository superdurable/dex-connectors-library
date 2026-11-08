# Shortcut connector

The Go `getStory` Query calls Shortcut REST API v3 `GET /stories/{story-public-id}` and returns only the Story ID, title, type, workflow state ID, and archived flag. Descriptions, comments, and member details are excluded. A missing Story selects `notFound`; 429, transport failures, and 5xx responses retry.

Create an API token in [Shortcut Account Settings > API Tokens](https://app.shortcut.com/settings/account/api-tokens). Enter it in Dex Web's secret `api_token` field. The public API endpoint, request timeout, and response limit have manifest defaults. See the [official REST API v3 reference](https://developer.shortcut.com/api/rest/v3).

The runnable Dex Flow is in [`go/examples/story-inspection`](go/examples/story-inspection/README.md). Its static connection name is `shortcut`. Python and TypeScript ports are not yet declared.
