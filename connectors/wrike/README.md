# Wrike connector

The Go, Python, and TypeScript `getTask` Query reads a Wrike task's ID, title, and status. Go v0.2.0 adds `listTasks` (safe), `createTask` (limited, three possible sends), `updateTask` (safe), and `deleteTask` (safe). Descriptions, custom fields, and assignees stay out of Dex Results. The Python and TypeScript packages remain at v0.1.0 with `getTask` only.

`listTasks` reads a folder page, optionally filtering by a title fragment; `pageSize` accepts up to 1000 and a blank `nextPageToken` starts at the first page. `createTask` takes a folder ID and nonempty title, with optional description. Wrike does not deduplicate task creation by call ID, so an unknown outcome can cause up to three sends before selecting `uncertain`. `updateTask` replaces title or status (`Active`, `Deferred`, `Completed`, or `Cancelled`). `deleteTask` moves a task to Wrike's Recycle Bin. All non-happy branches are optional and fail an example Flow when unwired. See the [Wrike task API](https://developers.wrike.com/reference/gettasksempty) for provider behavior.

## Connect

Sign in to [Wrike](https://www.wrike.com), open **profile > Apps & Integrations > API**, create or select an app, and create a Permanent Access Token. Copy its opaque secret once into Dex Web `api_token`. The connector sends it in the `Authorization: Bearer` header. The token inherits its user's permissions. See [Wrike permanent token setup](https://developers.wrike.com/docs/oauth-20-authorization).

Leave `baseUrl` blank for the displayed US API host `https://www.wrike.com/api/v4`. For EU accounts, enter `https://app-eu.wrike.com/api/v4`; confirm the account's host in Wrike. `timeout` is a per-request Go duration and `maxResponseBytes` is a byte limit; blank uses the displayed defaults. Find a task ID in a Wrike task URL or API response and pass it as `taskId`. See the [Wrike API v4 overview](https://developers.wrike.com/docs/overview) and [regional host guidance](https://developers.wrike.com/docs/faq).

## Examples and verification

Run the [Go](go/examples/task-inspection/README.md), [Python](python/examples/task_inspection/README.md), or [TypeScript](typescript/examples/task-inspection/README.md) Flow with the static `wrike` connection. Go-recorded conformance checks requests, branches, retries, and outputs for each language's declared operations. Local integration tests exercise real Dex with fixture providers. Real Wrike authorization and task writes remain unverified.
