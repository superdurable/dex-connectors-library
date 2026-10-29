# Figma connector

`github.com/superdurable/dex-connectors-library/connectors/figma` exposes read-only Dex Query Steps against Figma's public REST API. Its API key is resolved from the named connection for each call and is never placed in a URL, receipt, or Flow state.

## Connection

In Dex Web Connections, create a `figma` connection named `figma`. Open [Account menu > Settings > Security > Personal access tokens > Generate new token](https://www.figma.com/settings), create the token, and paste it into the secret `api_key` field. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. Figma needs both file_content:read and file_comments:read token scopes.

## Operations

| Operation | Official API reference |
| --- | --- |
| `getFile` | [Figma getFile](https://developers.figma.com/docs/rest-api/file-endpoints/) |
| `getFileComments` | [Figma getFileComments](https://developers.figma.com/docs/rest-api/comments-endpoints/) |

Each operation returns a bounded, typed response. A 404 on a single-resource read selects `notFound`; other definitive 4xx select `providerRejected`; 408, 409, 429, 5xx, and interrupted reads Retry under the default one-hour budget. Malformed and oversized JSON selects `invalidResponse`. Invalid local input or missing credentials selects `defect` before a request. Provider error bodies are not copied into failures.

## Mocks

The generated `figmamock` package includes one default Meridian answer per operation and an invalid-key rejection for each. The defaults describe a consistent set of project, lead, sheet, or file records.

## Example

The runnable [example](examples/figma-lookup/README.md) registers a Worker and a Flow using the static `figma` connection. CI runs its mock-backed real-Dex integration test; local unit tests verify deterministic mapping.

## Live verification

The API endpoint, authentication, fields, and HTTP statuses above were checked against the linked official docs. Provider authorization, account permissions, and live responses have not been exercised.
