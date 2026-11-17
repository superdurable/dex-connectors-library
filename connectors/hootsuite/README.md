# Hootsuite connector

The Go connector lists social profiles, reads scheduled posts, and schedules text posts. The [social profiles Flow](go/examples/social-profiles/README.md), [scheduled posts Flow](go/examples/scheduled-posts/README.md), and [post scheduling Flow](go/examples/post-scheduling/README.md) use the static `hootsuite` connection and operation-specific Step factories.

The [Python package](python/README.md) and [TypeScript package](typescript/README.md) implement the same three operations. Each has a runnable social profiles Flow. All three languages are checked against the Go-recorded provider scenarios.

## Connection setup

Create or select an app in the [Hootsuite Developer Portal](https://developer.hootsuite.com/) under **My Apps**. REST API access may require Hootsuite approval. Register the redirect URI displayed by Dex Web, then enter the app's Client ID and Client Secret in the Dex Web connection. Authorize with the `offline` scope so Dex can refresh the token. The authorizing Hootsuite user must be able to access the desired social profiles.

The API base URL, request timeout, and response size limit have manifest defaults. Only override them for a trusted gateway or a known operational need. Dex stores access and refresh tokens as secrets; do not put them in Flow inputs or results.

The [official Hootsuite API documentation](https://apidocs.hootsuite.com/docs/api/index.html) describes `GET /v1/socialProfiles`, `GET /v1/messages`, `POST /v1/messages`, and OAuth2 setup. The scheduled-post query accepts a UTC interval of at most four weeks and returns one page with a continuation cursor. The scheduling Mutation uses a single-dispatch fence that counts possible sends because Hootsuite offers no request idempotency key. An unknown outcome (a 5xx other than 503, 408, 409, any 4xx other than 400, 401, 403, 404, 411, 413, 414, 415, or 422, an interrupted or malformed response, or a connection lost after dispatch) retries and resends while possible sends stay below the operation's `maxSends` (default 3), so Planner may show a duplicate, and selects `uncertain` at the cap. A definite refusal selects `providerRejected` and is never resent; after an earlier possible send it selects `uncertain`. A response with a message ID but an incomplete body, or a saved message ID from an earlier attempt, selects `uncertain` without resending. An unreadable checkpoint retries without sending. Inspect Planner after `uncertain` before another post is sent. Provider authorization and live API behavior have not been verified in this checkout.
