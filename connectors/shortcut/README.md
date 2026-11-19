# Shortcut connector

The Go connector supports `getStory`, `searchStories`, `createStory`, `updateStory`, and `deleteStory` through Shortcut REST API v3. Search and get return only story ID, title, type, workflow state ID, and archived flag; descriptions, comments, and member details are excluded. A missing story selects `notFound`; 429, transport failures, and 5xx responses retry. Creation uses a bounded three-send fence because Shortcut does not document a server-side idempotency key. Absolute updates and deletes can safely retry.

Create an API token in [Shortcut Account Settings > API Tokens](https://app.shortcut.com/settings/account/api-tokens). Enter it in Dex Web's secret `api_token` field. The public API endpoint, request timeout, and response limit have manifest defaults. See the [official REST API v3 reference](https://developer.shortcut.com/api/rest/v3).

The runnable Dex Flows are in [`go/examples/story-inspection`](go/examples/story-inspection/README.md). Their static connection name is `shortcut`. Go releases at `v0.2.0`; Python and TypeScript remain at `v0.1.0` with only `getStory` implemented. Their conformance tests replay the `getStory` subset of the Go-recorded scenarios.
